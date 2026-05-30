package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	pgvector "github.com/pgvector/pgvector-go"
)

// saveJob holds the data passed from the HTTP handler to the background goroutine.
// No Valkey status tracking — the frontend doesn't poll; it refreshes the notes
// list on a short delay instead.
type saveJob struct {
	NoteID  int
	UserID  int
	Title   string
	Content string
	Tags    []string
}

// processSaveJob runs in a goroutine: embeds the content and writes to Postgres.
func (s *Server) processSaveJob(job saveJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	emb, err := s.embedText(ctx, job.Content)
	if err != nil {
		slog.Error("processSaveJob: embed failed", "note_id", job.NoteID, "user_id", job.UserID, "error", err)
		return
	}

	if job.NoteID == 0 {
		var id int
		err = s.db.QueryRow(ctx,
			`INSERT INTO notes (title, content, tags, embedding, user_id)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			job.Title, job.Content, job.Tags, pgvector.NewVector(emb), job.UserID,
		).Scan(&id)
		if err == nil {
			slog.Info("note created (async)", "id", id, "user_id", job.UserID)
		}
	} else {
		_, err = s.db.Exec(ctx,
			`UPDATE notes SET title=$2, content=$3, tags=$4, embedding=$5, updated_at=NOW()
			 WHERE id=$1 AND user_id=$6`,
			job.NoteID, job.Title, job.Content, job.Tags, pgvector.NewVector(emb), job.UserID,
		)
		if err == nil {
			slog.Info("note updated (async)", "id", job.NoteID, "user_id", job.UserID)
		}
	}

	if err != nil {
		slog.Error("processSaveJob: db write failed", "note_id", job.NoteID, "error", err)
		return
	}

	s.clearDraft(ctx, job.UserID, job.NoteID)
}

// ── HTTP handlers ──────────────────────────────────────────────────────────────

// POST /notes
func (s *Server) handleCreateNoteAsync(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	content := strings.TrimSpace(r.FormValue("content"))
	title := strings.TrimSpace(r.FormValue("title"))
	tags := parseTags(r.FormValue("tags"))

	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	go s.processSaveJob(saveJob{
		NoteID:  0,
		UserID:  user.UserID,
		Title:   title,
		Content: content,
		Tags:    tags,
	})

	s.closeModalAndScheduleRefresh(w, r)
}

// PUT /notes/{id}
func (s *Server) handleUpdateNoteAsync(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	title := strings.TrimSpace(r.FormValue("title"))
	tags := parseTags(r.FormValue("tags"))

	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	go s.processSaveJob(saveJob{
		NoteID:  id,
		UserID:  user.UserID,
		Title:   title,
		Content: content,
		Tags:    tags,
	})

	s.closeModalAndScheduleRefresh(w, r)
}

// closeModalAndScheduleRefresh closes the note modal immediately via OOB swap
// and triggers a notes list refresh after a short delay so the new/updated
// note appears once the background job has had time to complete.
func (s *Server) closeModalAndScheduleRefresh(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	w.Header().Set("HX-Trigger", "refresh-stats")

	s.render(w, "save_oob", map[string]any{
		"TagTree": s.cachedTagTree(r.Context(), user.UserID),
	})
}
