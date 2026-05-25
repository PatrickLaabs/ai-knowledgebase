package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// statusRecorder wraps ResponseWriter to capture the status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush delegates to the underlying ResponseWriter so SSE streaming works.
// Without this, w.(http.Flusher) in the chat handler fails and returns 500.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// sensitiveQueryKeys lists query-parameter names whose values must never be
// written to logs. Credentials should never appear in a URL at all, but this
// is defense in depth: if a form misconfiguration or a future endpoint ever
// puts one in the query string, the value is redacted before logging.
var sensitiveQueryKeys = map[string]bool{
	"password": true,
	"passwd":   true,
	"pass":     true,
	"token":    true,
	"secret":   true,
	"api_key":  true,
	"apikey":   true,
	"auth":     true,
}

// redactQuery parses a raw query string and replaces the values of any
// sensitive keys with "REDACTED", returning a safe-to-log version.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[unparseable]"
	}
	redacted := false
	for key := range values {
		if sensitiveQueryKeys[strings.ToLower(key)] {
			values.Set(key, "REDACTED")
			redacted = true
		}
	}
	if redacted {
		slog.Warn("sensitive data found in URL query string — check client code")
	}
	return values.Encode()
}

// loggingMiddleware logs method, path, status code, and duration for every
// request. Query strings are redacted so credentials never reach the logs.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", redactQuery(r.URL.RawQuery),
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}
