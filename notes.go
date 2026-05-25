package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	pgvector "github.com/pgvector/pgvector-go"
)

// GET /api/notes?tag=x&search=y
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	tag := r.URL.Query().Get("tag")
	search := r.URL.Query().Get("search")

	type NoteRow struct {
		ID        int      `json:"id"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
	}

	var rows pgx.Rows
	var err error
	start := time.Now()

	switch {
	case search != "":
		emb, embedErr := s.embedText(r.Context(), search)
		if embedErr != nil {
			http.Error(w, "embed error", http.StatusInternalServerError)
			return
		}
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			WHERE  embedding IS NOT NULL
			  AND  user_id = $2
			ORDER  BY embedding <=> $1
			LIMIT  50
		`, pgvector.NewVector(emb), user.UserID)

	case tag != "":
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			WHERE  user_id = $1
			  AND  EXISTS (SELECT 1 FROM unnest(tags) t WHERE t = $2 OR t LIKE $2 || '/%')
			ORDER  BY updated_at DESC
		`, user.UserID, tag)

	default:
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			WHERE  user_id = $1
			ORDER  BY updated_at DESC
		`, user.UserID)
	}

	if err != nil {
		slog.Error("list notes failed", "user_id", user.UserID, "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var notes []NoteRow
	for rows.Next() {
		var n NoteRow
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&n.ID, &n.Content, &n.Tags, &createdAt, &updatedAt); err == nil {
			n.CreatedAt = createdAt.Format(time.RFC3339)
			n.UpdatedAt = updatedAt.Format(time.RFC3339)
			notes = append(notes, n)
		}
	}
	if notes == nil {
		notes = []NoteRow{}
	}

	slog.Debug("notes listed",
		"user_id", user.UserID,
		"count", len(notes),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

// POST /api/notes
func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	var payload struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if payload.Tags == nil {
		payload.Tags = []string{}
	}

	emb, err := s.embedText(r.Context(), payload.Content)
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}

	var id int
	err = s.db.QueryRow(r.Context(),
		`INSERT INTO notes (content, tags, embedding, user_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		payload.Content, payload.Tags, pgvector.NewVector(emb), user.UserID,
	).Scan(&id)
	if err != nil {
		slog.Error("insert note failed", "user_id", user.UserID, "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	slog.Info("note created", "id", id, "user_id", user.UserID, "tags", payload.Tags)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"id": id})
}

// PUT /api/notes/{id}
func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var payload struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if payload.Tags == nil {
		payload.Tags = []string{}
	}

	emb, err := s.embedText(r.Context(), payload.Content)
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}

	// The user_id = $5 guard ensures users can only edit their own notes.
	tag, err := s.db.Exec(r.Context(),
		`UPDATE notes SET content=$2, tags=$3, embedding=$4, updated_at=NOW()
		 WHERE id=$1 AND user_id=$5`,
		id, payload.Content, payload.Tags, pgvector.NewVector(emb), user.UserID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		slog.Warn("update note: not found or not owned", "id", id, "user_id", user.UserID)
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}

	slog.Info("note updated", "id", id, "user_id", user.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/notes/{id}
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// user_id guard: users can only delete their own notes.
	tag, err := s.db.Exec(r.Context(),
		`DELETE FROM notes WHERE id=$1 AND user_id=$2`,
		id, user.UserID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		slog.Warn("delete note: not found or not owned", "id", id, "user_id", user.UserID)
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}

	slog.Info("note deleted", "id", id, "user_id", user.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/tags
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	rows, err := s.db.Query(r.Context(), `
		SELECT DISTINCT unnest(tags) AS tag
		FROM   notes
		WHERE  user_id = $1
		ORDER  BY tag
	`, user.UserID)
	if err != nil {
		slog.Error("list tags failed", "user_id", user.UserID, "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tags = append(tags, t)
		}
	}
	if tags == nil {
		tags = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}
