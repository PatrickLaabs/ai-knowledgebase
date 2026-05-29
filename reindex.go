package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	pgvector "github.com/pgvector/pgvector-go"
	"github.com/redis/go-redis/v9"
)

// reindexJobs guards against concurrent jobs per user.
// Only one reindex can run per user at a time.
var reindexJobs sync.Map // map[int]bool (userID -> running)

// ReindexStatus is persisted in Valkey (24h TTL) so the UI stays accurate
// even after navigating away and back.
type ReindexStatus struct {
	Status     string `json:"status"` // idle | running | done | error
	Total      int    `json:"total"`
	Completed  int    `json:"completed"`
	Failed     int    `json:"failed"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

func reindexJobKey(userID int) string {
	return fmt.Sprintf("reindex:job:%d", userID)
}

func (s *Server) getReindexStatus(ctx context.Context, userID int) (ReindexStatus, error) {
	val, err := s.rdb.Get(ctx, reindexJobKey(userID)).Result()
	if err == redis.Nil {
		return ReindexStatus{Status: "idle"}, nil
	}
	if err != nil {
		return ReindexStatus{}, fmt.Errorf("valkey get: %w", err)
	}
	var st ReindexStatus
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return ReindexStatus{}, err
	}
	return st, nil
}

func (s *Server) saveReindexStatus(ctx context.Context, userID int, st ReindexStatus) {
	data, _ := json.Marshal(st)
	if err := s.rdb.Set(ctx, reindexJobKey(userID), data, 24*time.Hour).Err(); err != nil {
		slog.Warn("reindex: failed to persist status", "user_id", userID, "error", err)
	}
}

// POST /api/admin/reindex
func (s *Server) handleStartReindex(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	// Atomic per-user lock using sync.Map.
	if _, loaded := reindexJobs.LoadOrStore(user.UserID, true); loaded {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "reindex already running"})
		return
	}

	st := ReindexStatus{
		Status:    "running",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.saveReindexStatus(r.Context(), user.UserID, st)
	slog.Info("reindex: job accepted", "user", user.Username)

	go s.runReindex(user.UserID, user.Username, st)

	w.Header().Set("HX-Trigger", "refresh-stats")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(st)
}

// GET /api/admin/reindex/status
func (s *Server) handleReindexStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	st, err := s.getReindexStatus(r.Context(), user.UserID)
	if err != nil {
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

// runReindex re-embeds all notes belonging to userID and writes progress to
// Valkey after every note so the UI stays live during long jobs.
func (s *Server) runReindex(userID int, username string, st ReindexStatus) {
	defer reindexJobs.Delete(userID)
	ctx := context.Background()

	// Count scoped to this user.
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notes WHERE user_id = $1`, userID,
	).Scan(&st.Total); err != nil {
		slog.Error("reindex: count failed", "user", username, "error", err)
		st.Status = "error"
		st.Error = err.Error()
		st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.saveReindexStatus(ctx, userID, st)
		return
	}
	s.saveReindexStatus(ctx, userID, st)
	slog.Info("reindex: worker started", "user", username, "total", st.Total)

	// Load all note IDs up front to avoid holding a cursor open during embedding.
	rows, err := s.db.Query(ctx,
		`SELECT id, content FROM notes WHERE user_id = $1 ORDER BY id`, userID,
	)
	if err != nil {
		st.Status = "error"
		st.Error = err.Error()
		st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.saveReindexStatus(ctx, userID, st)
		return
	}

	type noteRow struct {
		ID      int
		Content string
	}
	var notes []noteRow
	for rows.Next() {
		var n noteRow
		if rows.Scan(&n.ID, &n.Content) == nil {
			notes = append(notes, n)
		}
	}
	rows.Close()

	for _, n := range notes {
		emb, err := s.embedText(ctx, n.Content)
		if err != nil {
			slog.Warn("reindex: embed failed", "note_id", n.ID, "user", username, "error", err)
			st.Failed++
		} else if _, err = s.db.Exec(ctx,
			`UPDATE notes SET embedding=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`,
			pgvector.NewVector(emb), n.ID, userID,
		); err != nil {
			slog.Warn("reindex: db update failed", "note_id", n.ID, "error", err)
			st.Failed++
		} else {
			st.Completed++
		}

		pct := float64(st.Completed+st.Failed) / float64(st.Total) * 100
		slog.Info("reindex: progress",
			"user", username,
			"completed", st.Completed,
			"failed", st.Failed,
			"total", st.Total,
			"progress", fmt.Sprintf("%.1f%%", pct),
		)
		s.saveReindexStatus(ctx, userID, st)
	}

	st.Status = "done"
	st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.rdb.Set(ctx, "reindex:last:"+itoa(userID), time.Now().UTC().Format(time.RFC3339), 0)
	s.saveReindexStatus(ctx, userID, st)
	slog.Info("reindex: complete", "user", username, "completed", st.Completed, "failed", st.Failed)
}
