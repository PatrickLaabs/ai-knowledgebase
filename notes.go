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
		slog.Debug("listing notes via semantic search", "query", search)
		emb, embedErr := s.embedText(r.Context(), search)
		if embedErr != nil {
			http.Error(w, "embed error", http.StatusInternalServerError)
			return
		}
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			WHERE  embedding IS NOT NULL
			ORDER  BY embedding <=> $1
			LIMIT  50
		`, pgvector.NewVector(emb))

	case tag != "":
		slog.Debug("listing notes by tag", "tag", tag)
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			WHERE  EXISTS (SELECT 1 FROM unnest(tags) t WHERE t = $1 OR t LIKE $1 || '/%')
			ORDER  BY updated_at DESC
		`, tag)

	default:
		slog.Debug("listing all notes")
		rows, err = s.db.Query(r.Context(), `
			SELECT id, content, tags, created_at, updated_at
			FROM   notes
			ORDER  BY updated_at DESC
		`)
	}

	if err != nil {
		slog.Error("list notes query failed", "error", err)
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
		"count", len(notes),
		"filter", map[string]string{"tag": tag, "search": search},
		"duration_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

// POST /api/notes
func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
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

	start := time.Now()
	var id int
	err = s.db.QueryRow(r.Context(),
		`INSERT INTO notes (content, tags, embedding) VALUES ($1, $2, $3) RETURNING id`,
		payload.Content, payload.Tags, pgvector.NewVector(emb),
	).Scan(&id)
	if err != nil {
		slog.Error("insert note failed", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	slog.Info("note created",
		"id", id,
		"tags", payload.Tags,
		"content_len", len(payload.Content),
		"db_duration_ms", time.Since(start).Milliseconds(),
	)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"id": id})
}

// PUT /api/notes/{id}
func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
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

	start := time.Now()
	tag, err := s.db.Exec(r.Context(),
		`UPDATE notes SET content=$2, tags=$3, embedding=$4, updated_at=NOW() WHERE id=$1`,
		id, payload.Content, payload.Tags, pgvector.NewVector(emb),
	)
	if err != nil || tag.RowsAffected() == 0 {
		slog.Error("update note failed", "id", id, "error", err)
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}

	slog.Info("note updated",
		"id", id,
		"tags", payload.Tags,
		"content_len", len(payload.Content),
		"db_duration_ms", time.Since(start).Milliseconds(),
	)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/notes/{id}
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	start := time.Now()
	tag, err := s.db.Exec(r.Context(), `DELETE FROM notes WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		slog.Error("delete note failed", "id", id, "error", err)
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}

	slog.Info("note deleted", "id", id, "db_duration_ms", time.Since(start).Milliseconds())
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/tags
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT DISTINCT unnest(tags) AS tag FROM notes ORDER BY tag
	`)
	if err != nil {
		slog.Error("list tags failed", "error", err)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}
