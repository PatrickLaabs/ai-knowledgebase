package main

import (
	"log/slog"
	"net/http"
	"time"
)

// IndexStats summarises the indexing state of a user's notes.
type IndexStats struct {
	TotalNotes   int
	IndexedNotes int
	Unindexed    int
	TotalTags    int
	Percent      int    // 0-100, indexed / total
	EmbedModel   string // model name from config
	LastReindex  string // human-readable, or "never"
}

// queryIndexStats gathers counts in a single round-trip where possible.
func (s *Server) queryIndexStats(r *http.Request, userID int) IndexStats {
	ctx := r.Context()
	stats := IndexStats{EmbedModel: EmbedModel, LastReindex: "never"}

	// Notes counts: total + how many have an embedding.
	err := s.db.QueryRow(ctx, `
		SELECT
		  COUNT(*),
		  COUNT(*) FILTER (WHERE embedding IS NOT NULL)
		FROM notes WHERE user_id = $1
	`, userID).Scan(&stats.TotalNotes, &stats.IndexedNotes)
	if err != nil {
		slog.Error("queryIndexStats: count failed", "user_id", userID, "error", err)
		return stats
	}
	stats.Unindexed = stats.TotalNotes - stats.IndexedNotes
	if stats.TotalNotes > 0 {
		stats.Percent = (stats.IndexedNotes * 100) / stats.TotalNotes
	} else {
		stats.Percent = 100 // no notes = nothing pending = fully "done"
	}

	// Distinct tag count.
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT tag)
		FROM notes, unnest(tags) AS tag
		WHERE user_id = $1
	`, userID).Scan(&stats.TotalTags); err != nil {
		slog.Warn("queryIndexStats: tag count failed", "error", err)
	}

	// Last reindex time, stored in Valkey by the reindex job (if present).
	if ts, err := s.rdb.Get(ctx, "reindex:last:"+itoa(userID)).Result(); err == nil && ts != "" {
		if parsed, perr := time.Parse(time.RFC3339, ts); perr == nil {
			stats.LastReindex = humanizeSince(parsed)
		}
	}

	return stats
}

// GET /api/stats — returns the stats panel partial.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	s.render(w, "stats_panel", s.queryIndexStats(r, user.UserID))
}

// humanizeSince turns a timestamp into "3m ago", "2h ago", "5d ago".
func humanizeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours()/24)) + "d ago"
	}
}

// itoa is a tiny strconv.Itoa wrapper kept local to avoid an import churn.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
