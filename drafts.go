package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const draftTTL = 24 * time.Hour

// draftKey returns the Valkey key for a user's draft.
// noteID == 0 means a "new note" draft.
func draftKey(userID, noteID int) string {
	if noteID == 0 {
		return fmt.Sprintf("draft:%d:new", userID)
	}
	return fmt.Sprintf("draft:%d:%d", userID, noteID)
}

type Draft struct {
	Content string `json:"content"`
	Tags    string `json:"tags"` // raw comma-separated string, as typed
}

// saveDraft persists the draft to Valkey, sliding the TTL.
func (s *Server) saveDraft(ctx context.Context, userID, noteID int, d Draft) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, draftKey(userID, noteID), data, draftTTL).Err()
}

// loadDraft retrieves a draft from Valkey. Returns nil (no error) if absent.
func (s *Server) loadDraft(ctx context.Context, userID, noteID int) (*Draft, error) {
	data, err := s.rdb.Get(ctx, draftKey(userID, noteID)).Bytes()
	if err != nil {
		return nil, nil // key missing — not an error
	}
	var d Draft
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, nil // corrupt draft — ignore it
	}
	return &d, nil
}

// clearDraft deletes the draft key after a successful save.
func (s *Server) clearDraft(ctx context.Context, userID, noteID int) {
	if err := s.rdb.Del(ctx, draftKey(userID, noteID)).Err(); err != nil {
		slog.Warn("clearDraft: failed to delete key",
			"user_id", userID, "note_id", noteID, "error", err)
	}
}

// ── HTTP handlers ──────────────────────────────────────────────────────────────

// POST /drafts/save?id=0
// Called by htmx on keyup delay from the note form textarea.
// Returns the "draft_indicator" partial so the UI shows save status.
func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	noteID, _ := strconv.Atoi(r.URL.Query().Get("id")) // 0 for new notes

	d := Draft{
		Content: r.FormValue("content"),
		Tags:    r.FormValue("tags"),
	}

	if err := s.saveDraft(r.Context(), user.UserID, noteID, d); err != nil {
		slog.Warn("handleSaveDraft: save failed",
			"user_id", user.UserID, "note_id", noteID, "error", err)
		s.render(w, "draft_indicator", map[string]any{"Status": "error"})
		return
	}

	slog.Debug("draft saved", "user_id", user.UserID, "note_id", noteID)
	s.render(w, "draft_indicator", map[string]any{
		"Status":  "saved",
		"SavedAt": time.Now().Format("15:04:05"),
	})
}

// DELETE /drafts?id=0
// Called explicitly when the user cancels the form to discard the draft.
func (s *Server) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	noteID, _ := strconv.Atoi(r.URL.Query().Get("id"))
	s.clearDraft(r.Context(), user.UserID, noteID)
	w.WriteHeader(http.StatusOK)
}
