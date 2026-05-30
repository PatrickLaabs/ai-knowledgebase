package main

import (
	"net/http"
	"strings"
)

// GET /tags/suggest?q=kub — returns a dropdown partial of matching tags.
func (s *Server) handleTagSuggest(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	if query == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Fetch all tags for this user. Uses the existing queryTagTree path
	// which hits Postgres (or Valkey cache if tagtree_cache.go is active).
	rows, err := s.db.Query(r.Context(), `
		SELECT DISTINCT unnest(tags) AS tag
		FROM notes WHERE user_id = $1 ORDER BY tag
	`, user.UserID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var tag string
		if rows.Scan(&tag) == nil {
			if strings.Contains(strings.ToLower(tag), query) {
				matches = append(matches, tag)
			}
		}
	}

	if len(matches) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.render(w, "tag_suggestions", matches)
}
