package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/redis/go-redis/v9"
)

const reindexJobKey = "reindex:job"

// ReindexStatus is persisted in Valkey so the UI can poll progress even after
// navigating away and back. TTL is 24h so stale jobs auto-clean themselves.
type ReindexStatus struct {
	Status     string `json:"status"` // idle | running | done | error
	Total      int    `json:"total"`
	Completed  int    `json:"completed"`
	Failed     int    `json:"failed"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

// reindexRunning guards against concurrent jobs. CompareAndSwap ensures only
// one goroutine can start a job even under concurrent POST /api/admin/reindex.
var reindexRunning atomic.Bool

func (s *Server) getReindexStatus(ctx context.Context) (ReindexStatus, error) {
	val, err := s.rdb.Get(ctx, reindexJobKey).Result()
	if errors.Is(err, redis.Nil) {
		return ReindexStatus{Status: "idle"}, nil
	}
	if err != nil {
		return ReindexStatus{}, fmt.Errorf("valkey get: %w", err)
	}
	var st ReindexStatus
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return ReindexStatus{}, fmt.Errorf("unmarshal status: %w", err)
	}
	return st, nil
}

func (s *Server) saveReindexStatus(ctx context.Context, st ReindexStatus) {
	data, _ := json.Marshal(st)
	if err := s.rdb.Set(ctx, reindexJobKey, data, 24*time.Hour).Err(); err != nil {
		slog.Warn("reindex: failed to persist status to valkey", "error", err)
	}
}

// POST /api/admin/reindex
// Kicks off a background re-embedding job. Returns 409 if already running.
func (s *Server) handleStartReindex(w http.ResponseWriter, r *http.Request) {
	if !reindexRunning.CompareAndSwap(false, true) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		err := json.NewEncoder(w).Encode(map[string]string{"error": "reindex already running"})
		if err != nil {
			return
		}
		return
	}

	st := ReindexStatus{
		Status:    "running",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.saveReindexStatus(r.Context(), st)
	slog.Info("reindex: job accepted, launching background worker")

	go s.runReindex(st)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	err := json.NewEncoder(w).Encode(st)
	if err != nil {
		return
	}
}

// GET /api/admin/reindex/status
// Returns current job state from Valkey. Safe to poll every 2s from the UI.
func (s *Server) handleReindexStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.getReindexStatus(r.Context())
	if err != nil {
		slog.Error("reindex: status read failed", "error", err)
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(st)
	if err != nil {
		return
	}
}

// runReindex is the background worker. It embeds every note sequentially and
// writes granular progress to Valkey after each note so the UI stays live.
func (s *Server) runReindex(st ReindexStatus) {
	defer reindexRunning.Store(false)
	ctx := context.Background()

	// ── 1. Count total so the UI can show a progress bar immediately ──────────
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM notes`).Scan(&st.Total); err != nil {
		slog.Error("reindex: count query failed", "error", err)
		st.Status = "error"
		st.Error = err.Error()
		st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.saveReindexStatus(ctx, st)
		return
	}
	s.saveReindexStatus(ctx, st)
	slog.Info("reindex: worker started", "total_notes", st.Total)

	// ── 2. Load all note IDs + content up front (avoids holding a cursor open
	//       for the entire embedding duration, which can be many minutes) ──────
	rows, err := s.db.Query(ctx, `SELECT id, content FROM notes ORDER BY id`)
	if err != nil {
		slog.Error("reindex: notes fetch failed", "error", err)
		st.Status = "error"
		st.Error = err.Error()
		st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.saveReindexStatus(ctx, st)
		return
	}

	type noteRow struct {
		ID      int
		Content string
	}
	var notes []noteRow
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.ID, &n.Content); err == nil {
			notes = append(notes, n)
		}
	}
	rows.Close()

	// ── 3. Embed each note, updating Valkey after every one ───────────────────
	for _, n := range notes {
		emb, err := s.embedText(ctx, n.Content)
		if err != nil {
			slog.Warn("reindex: embed failed",
				"note_id", n.ID,
				"error", err,
				"completed", st.Completed,
				"failed", st.Failed+1,
				"total", st.Total,
			)
			st.Failed++
		} else {
			if _, err = s.db.Exec(ctx,
				`UPDATE notes SET embedding = $1, updated_at = NOW() WHERE id = $2`,
				pgvector.NewVector(emb), n.ID,
			); err != nil {
				slog.Warn("reindex: db update failed",
					"note_id", n.ID,
					"error", err,
				)
				st.Failed++
			} else {
				st.Completed++
				pct := float64(st.Completed+st.Failed) / float64(st.Total) * 100
				slog.Info("reindex: note embedded",
					"note_id", n.ID,
					"completed", st.Completed,
					"failed", st.Failed,
					"total", st.Total,
					"progress", fmt.Sprintf("%.1f%%", pct),
				)
			}
		}

		// Persist after every note — UI polls this every 2s
		s.saveReindexStatus(ctx, st)
	}

	// ── 4. Mark complete ──────────────────────────────────────────────────────
	st.Status = "done"
	st.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.saveReindexStatus(ctx, st)

	slog.Info("reindex: complete",
		"completed", st.Completed,
		"failed", st.Failed,
		"total", st.Total,
	)
}
