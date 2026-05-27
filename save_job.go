package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	pgvector "github.com/pgvector/pgvector-go"
)

const (
	saveJobTTL    = 10 * time.Minute
	saveJobPrefix = "savejob:"
)

type SaveJobStatus string

const (
	SaveJobPending SaveJobStatus = "pending"
	SaveJobDone    SaveJobStatus = "done"
	SaveJobError   SaveJobStatus = "error"
)

type SaveJob struct {
	Status  SaveJobStatus `json:"status"`
	NoteID  int           `json:"note_id"` // 0 = new note
	UserID  int           `json:"user_id"`
	Content string        `json:"content"`
	Tags    []string      `json:"tags"`
	Error   string        `json:"error,omitempty"`
}

func saveJobKey(jobID string) string {
	return saveJobPrefix + jobID
}

func (s *Server) writeJob(ctx context.Context, jobID string, job SaveJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, saveJobKey(jobID), data, saveJobTTL).Err()
}

func (s *Server) readJob(ctx context.Context, jobID string) (*SaveJob, error) {
	data, err := s.rdb.Get(ctx, saveJobKey(jobID)).Bytes()
	if err != nil {
		return nil, err
	}
	var job SaveJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// processSaveJob runs in a goroutine: embeds the content and writes to Postgres,
// then updates the job status in Valkey so the poller picks it up.
func (s *Server) processSaveJob(jobID string, job SaveJob) {
	// Use a fresh background context — the HTTP request context is already done.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	emb, err := s.embedText(ctx, job.Content)
	if err != nil {
		slog.Error("processSaveJob: embed failed", "job_id", jobID, "error", err)
		job.Status = SaveJobError
		job.Error = "embedding failed: " + err.Error()
		s.writeJob(ctx, jobID, job) //nolint
		return
	}

	if job.NoteID == 0 {
		// Create
		var id int
		err = s.db.QueryRow(ctx,
			`INSERT INTO notes (content, tags, embedding, user_id)
			 VALUES ($1, $2, $3, $4) RETURNING id`,
			job.Content, job.Tags, pgvector.NewVector(emb), job.UserID,
		).Scan(&id)
		if err == nil {
			job.NoteID = id
			slog.Info("note created (async)", "id", id, "user_id", job.UserID)
		}
	} else {
		// Update
		_, err = s.db.Exec(ctx,
			`UPDATE notes SET content=$2, tags=$3, embedding=$4, updated_at=NOW()
			 WHERE id=$1 AND user_id=$5`,
			job.NoteID, job.Content, job.Tags, pgvector.NewVector(emb), job.UserID,
		)
		if err == nil {
			slog.Info("note updated (async)", "id", job.NoteID, "user_id", job.UserID)
		}
	}

	if err != nil {
		slog.Error("processSaveJob: db write failed", "job_id", jobID, "error", err)
		job.Status = SaveJobError
		job.Error = "database write failed"
	} else {
		job.Status = SaveJobDone
		// Clear the draft now that the note is safely in Postgres.
		s.clearDraft(ctx, job.UserID, job.NoteID)
	}

	if err := s.writeJob(ctx, jobID, job); err != nil {
		slog.Error("processSaveJob: status update failed", "job_id", jobID, "error", err)
	}
}

// ── HTTP handlers ──────────────────────────────────────────────────────────────

// POST /notes  (async version — replaces handleCreateNotePartial)
func (s *Server) handleCreateNoteAsync(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	content := r.FormValue("content")
	tags := parseTags(r.FormValue("tags"))

	if len(content) == 0 {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("%d-%d", user.UserID, time.Now().UnixNano())
	job := SaveJob{
		Status:  SaveJobPending,
		NoteID:  0,
		UserID:  user.UserID,
		Content: content,
		Tags:    tags,
	}
	if err := s.writeJob(r.Context(), jobID, job); err != nil {
		slog.Error("handleCreateNoteAsync: writeJob failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	go s.processSaveJob(jobID, job)

	// Close the modal immediately — work continues in the background.
	// The notes list will refresh automatically once the job completes.
	s.closeModalAndScheduleRefresh(w, r)
}

// PUT /notes/{id}  (async version — replaces handleUpdateNotePartial)
func (s *Server) handleUpdateNoteAsync(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	content := r.FormValue("content")
	tags := parseTags(r.FormValue("tags"))

	if len(content) == 0 {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("%d-%d-%d", user.UserID, id, time.Now().UnixNano())
	job := SaveJob{
		Status:  SaveJobPending,
		NoteID:  id,
		UserID:  user.UserID,
		Content: content,
		Tags:    tags,
	}
	if err := s.writeJob(r.Context(), jobID, job); err != nil {
		slog.Error("handleUpdateNoteAsync: writeJob failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	go s.processSaveJob(jobID, job)

	// Close the modal immediately — work continues in the background.
	s.closeModalAndScheduleRefresh(w, r)
}

// GET /notes/save-status/{jobID}
// htmx polls this every 1.5s. Returns:
//   - save_status partial again (still pending)  → htmx keeps polling
//   - notes_list + OOB modal clear (done)        → htmx swaps in, polling stops
//   - save_error partial (error)                 → htmx swaps in, polling stops
func (s *Server) handleSaveStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	jobID := r.PathValue("jobID")

	job, err := s.readJob(r.Context(), jobID)
	if err != nil {
		s.render(w, "save_error", map[string]any{"Error": "Job not found — please try saving again."})
		return
	}

	switch job.Status {
	case SaveJobPending:
		// Still processing — return empty so the sidebar spinner keeps showing.
		w.WriteHeader(http.StatusOK)

	case SaveJobDone:
		// Done — trigger a final notes list refresh to pick up the saved note.
		notes, _ := s.queryNotes(r, user.UserID, "", "")
		s.render(w, "notes_list", notes)

	case SaveJobError:
		// Surface the error as a toast in the sidebar.
		s.render(w, "save_error", map[string]any{"Error": job.Error})
	}
}

// closeModalAndScheduleRefresh closes the note modal immediately via OOB swap
// and triggers a notes list refresh after a short delay so the new/updated
// note appears once the background job has had time to complete.
func (s *Server) closeModalAndScheduleRefresh(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// OOB: clear the modal right away.
	fmt.Fprint(w, `<div id="note-modal" hx-swap-oob="true"></div>`)

	// OOB: replace notes list with a self-refreshing placeholder that polls
	// once after 2 seconds — enough time for embedding to finish on most notes.
	// On completion it swaps in the real list and stops polling.
	fmt.Fprintf(w, `<div id="notes-list" hx-swap-oob="true"
	     hx-get="/notes"
	     hx-trigger="load delay:2s"
	     hx-target="#notes-list"
	     hx-swap="outerHTML">
	  <div class="px-4 py-8 text-xs text-gray-600 text-center">
	    <svg class="animate-spin h-4 w-4 text-indigo-500 mx-auto mb-2"
	         xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
	      <circle class="opacity-25" cx="12" cy="12" r="10"
	              stroke="currentColor" stroke-width="4"></circle>
	      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
	    </svg>
	    Saving…
	  </div>
	</div>`)

	// Also refresh the tag tree OOB in case new tags were added.
	fmt.Fprint(w, `<div id="tag-tree" hx-swap-oob="true">`)
	tmpl.ExecuteTemplate(w, "tag_tree", s.queryTagTree(r, user.UserID))
	fmt.Fprint(w, `</div>`)

	// Primary target: empty (the form submitted to #save-status-target,
	// which gets cleared out since we're done with it).
	fmt.Fprint(w, `<div id="save-status-target"></div>`)
}
