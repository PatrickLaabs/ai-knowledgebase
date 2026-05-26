package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	pgvector "github.com/pgvector/pgvector-go"
	"golang.org/x/crypto/bcrypt"
)

// Note is the template-friendly struct used by all HTML/htmx handlers.
type Note struct {
	ID        int
	Content   string
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
	HasDraft  bool // true when Valkey holds unsaved edits for this note
}

// TagNode is one node in the hierarchical tag tree.
type TagNode struct {
	Label    string // display segment, e.g. "fluxcd"
	FullPath string // full tag path, e.g. "kubernetes/fluxcd"
	Children []*TagNode
}

// buildTagTree converts a flat slice of tag paths ("kubernetes/fluxcd")
// into a nested []*TagNode suitable for recursive template rendering.
func buildTagTree(tags []string) []*TagNode {
	root := &TagNode{}
	for _, tag := range tags {
		parts := strings.Split(tag, "/")
		cur := root
		for i, part := range parts {
			var found *TagNode
			for _, c := range cur.Children {
				if c.Label == part {
					found = c
					break
				}
			}
			if found == nil {
				found = &TagNode{
					Label:    part,
					FullPath: strings.Join(parts[:i+1], "/"),
				}
				cur.Children = append(cur.Children, found)
			}
			cur = found
		}
	}
	return root.Children
}

// queryTagTree fetches all distinct tags for a user and returns a tree.
func (s *Server) queryTagTree(r *http.Request, userID int) []*TagNode {
	rows, err := s.db.Query(r.Context(), `
		SELECT DISTINCT unnest(tags) AS tag
		FROM notes WHERE user_id = $1 ORDER BY tag
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tags = append(tags, t)
		}
	}
	return buildTagTree(tags)
}

// ── Full page handlers ─────────────────────────────────────────────────────────

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	sess, _ := s.sessionFromRequest(r)
	if sess != nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.sessionFromRequest(r)
	if sess != nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	s.render(w, "login", map[string]any{})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	var id int
	var hash string
	err := s.db.QueryRow(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = $1`, username,
	).Scan(&id, &hash)
	if err != nil {
		bcrypt.CompareHashAndPassword([]byte("$2a$12$dummydummydummydummydummydummydummydummydu"), []byte(password)) //nolint
		s.render(w, "auth_error", map[string]any{"Error": "Invalid username or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		s.render(w, "auth_error", map[string]any{"Error": "Invalid username or password"})
		return
	}
	token, err := s.createSession(r.Context(), id, username)
	if err != nil {
		s.render(w, "auth_error", map[string]any{"Error": "Server error, please try again"})
		return
	}
	s.setSessionCookie(w, token)
	slog.Info("user logged in (html)", "username", username)
	w.Header().Set("HX-Redirect", "/app")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if !AllowRegistration {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	sess, _ := s.sessionFromRequest(r)
	if sess != nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	s.render(w, "register", map[string]any{})
}

func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	if !AllowRegistration {
		s.render(w, "auth_error", map[string]any{"Error": "Registration is disabled"})
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if len(username) < 2 || len(password) < 8 {
		s.render(w, "auth_error", map[string]any{"Error": "Username ≥ 2 chars, password ≥ 8 chars"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.render(w, "auth_error", map[string]any{"Error": "Server error"})
		return
	}
	var id int
	if err := s.db.QueryRow(r.Context(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, string(hash),
	).Scan(&id); err != nil {
		s.render(w, "auth_error", map[string]any{"Error": "Username already taken"})
		return
	}
	token, err := s.createSession(r.Context(), id, username)
	if err != nil {
		s.render(w, "auth_error", map[string]any{"Error": "Server error"})
		return
	}
	s.setSessionCookie(w, token)
	slog.Info("user registered (html)", "username", username, "id", id)
	w.Header().Set("HX-Redirect", "/app")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		s.deleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
	w.Header().Set("HX-Redirect", "/login")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAppPage(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	notes, _ := s.queryNotes(r, user.UserID, "", "")
	tagTree := s.queryTagTree(r, user.UserID)
	s.render(w, "app", map[string]any{
		"User":    user,
		"Notes":   notes,
		"TagTree": tagTree,
	})
}

// ── htmx partial handlers ──────────────────────────────────────────────────────

// GET /notes?search=x&tag=y
func (s *Server) handleNotesPartial(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	notes, _ := s.queryNotes(r, user.UserID, r.URL.Query().Get("tag"), r.URL.Query().Get("search"))
	s.render(w, "notes_list", notes)
}

// GET /tags/tree — returns refreshed tag tree partial after a note is saved.
func (s *Server) handleTagTreePartial(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	s.render(w, "tag_tree", s.queryTagTree(r, user.UserID))
}

// GET /notes/new
func (s *Server) handleNoteNewForm(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	n := &Note{}
	if draft, _ := s.loadDraft(r.Context(), user.UserID, 0); draft != nil {
		n.Content = draft.Content
		n.Tags = parseTags(draft.Tags)
		n.HasDraft = true
	}
	s.render(w, "note_form", n)
}

// GET /notes/{id}/edit
func (s *Server) handleNoteEditForm(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var n Note
	if err := s.db.QueryRow(r.Context(),
		`SELECT id, content, tags, created_at, updated_at FROM notes WHERE id=$1 AND user_id=$2`,
		id, user.UserID,
	).Scan(&n.ID, &n.Content, &n.Tags, &n.CreatedAt, &n.UpdatedAt); err != nil {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	if draft, _ := s.loadDraft(r.Context(), user.UserID, id); draft != nil {
		n.Content = draft.Content
		n.Tags = parseTags(draft.Tags)
		n.HasDraft = true
	}
	s.render(w, "note_form", &n)
}

// POST /notes
func (s *Server) handleCreateNotePartial(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}
	tags := parseTags(r.FormValue("tags"))
	emb, err := s.embedText(r.Context(), content)
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}
	var id int
	if err := s.db.QueryRow(r.Context(),
		`INSERT INTO notes (content, tags, embedding, user_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		content, tags, pgvector.NewVector(emb), user.UserID,
	).Scan(&id); err != nil {
		slog.Error("createNotePartial: insert failed", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.clearDraft(r.Context(), user.UserID, 0)
	slog.Info("note created (html)", "id", id, "user_id", user.UserID)
	s.refreshAndCloseModal(w, r, user.UserID)
}

// PUT /notes/{id}
func (s *Server) handleUpdateNotePartial(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}
	tags := parseTags(r.FormValue("tags"))
	emb, err := s.embedText(r.Context(), content)
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}
	res, err := s.db.Exec(r.Context(),
		`UPDATE notes SET content=$2, tags=$3, embedding=$4, updated_at=NOW() WHERE id=$1 AND user_id=$5`,
		id, content, tags, pgvector.NewVector(emb), user.UserID,
	)
	if err != nil || res.RowsAffected() == 0 {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	s.clearDraft(r.Context(), user.UserID, id)
	slog.Info("note updated (html)", "id", id, "user_id", user.UserID)
	s.refreshAndCloseModal(w, r, user.UserID)
}

func (s *Server) handleDeleteNotePartial(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := s.db.Exec(r.Context(),
		`DELETE FROM notes WHERE id=$1 AND user_id=$2`, id, user.UserID,
	)
	if err != nil || res.RowsAffected() == 0 {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	slog.Info("note deleted (html)", "id", id, "user_id", user.UserID)

	// Reuse refreshAndCloseModal — it already does OOB tag tree + notes list.
	// Modal is already closed (delete comes from the list, not the form),
	// so the empty OOB modal div is harmless.
	s.refreshAndCloseModal(w, r, user.UserID)
}

func (s *Server) handleEmpty(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// refreshAndCloseModal writes the updated notes list as the primary htmx swap
// target, plus an out-of-band swap that empties #note-modal.
// This is the fix for bugs 1 & 3: no JS, no hx-on::after-request needed.
func (s *Server) refreshAndCloseModal(w http.ResponseWriter, r *http.Request, userID int) {
	notes, err := s.queryNotes(r, userID, "", "")
	if err != nil {
		slog.Error("refreshAndCloseModal: query failed", "error", err)
		notes = []Note{}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// OOB swap: clear the modal without touching the notes list target.
	fmt.Fprint(w, `<div id="note-modal" hx-swap-oob="true"></div>`)
	// Also refresh the tag tree OOB so new tags appear immediately.
	fmt.Fprint(w, `<div id="tag-tree" hx-swap-oob="true">`)
	tmpl.ExecuteTemplate(w, "tag_tree", s.queryTagTree(r, userID))
	fmt.Fprint(w, `</div>`)
	// Primary target (#notes-list): the updated notes list.
	tmpl.ExecuteTemplate(w, "notes_list", notes)
}

// queryNotes fetches notes with optional tag/search filters.
func (s *Server) queryNotes(r *http.Request, userID int, tag, search string) ([]Note, error) {
	ctx := r.Context()
	var sqlStr string
	var args []any
	switch {
	case search != "":
		emb, err := s.embedText(ctx, search)
		if err != nil {
			return nil, err
		}
		sqlStr = `SELECT id, content, tags, created_at, updated_at
		          FROM notes WHERE embedding IS NOT NULL AND user_id = $2
		          ORDER BY embedding <=> $1 LIMIT 50`
		args = []any{pgvector.NewVector(emb), userID}
	case tag != "":
		sqlStr = `SELECT id, content, tags, created_at, updated_at
		          FROM notes WHERE user_id = $1
		            AND EXISTS (SELECT 1 FROM unnest(tags) t WHERE t = $2 OR t LIKE $2 || '/%')
		          ORDER BY updated_at DESC`
		args = []any{userID, tag}
	default:
		sqlStr = `SELECT id, content, tags, created_at, updated_at
		          FROM notes WHERE user_id = $1 ORDER BY updated_at DESC`
		args = []any{userID}
	}
	rows, err := s.db.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Content, &n.Tags, &n.CreatedAt, &n.UpdatedAt); err == nil {
			if n.Tags == nil {
				n.Tags = []string{}
			}
			notes = append(notes, n)
		}
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, rows.Err()
}

// parseTags splits a comma-separated string into a cleaned slice.
func parseTags(raw string) []string {
	var tags []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			tags = append(tags, t)
		}
	}
	if tags == nil {
		return []string{}
	}
	return tags
}
