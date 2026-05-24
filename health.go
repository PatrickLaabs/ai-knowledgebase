package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// GET /api/health
// Returns 200 + {"status":"ok"} when the DB is reachable, 503 otherwise.
// Used by the UI status dot and can be wired to Kubernetes liveness probes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := s.db.Ping(r.Context()); err != nil {
		slog.Warn("health check: db ping failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
