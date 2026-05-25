package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "kb_session"
	sessionTTL        = 7 * 24 * time.Hour
	bcryptCost        = 12
)

// ── Context key ───────────────────────────────────────────────────────────────

type contextKey string

const ctxUser contextKey = "user"

// Session is the value stored in Valkey and attached to every authenticated
// request context. Handlers retrieve it via userFromContext.
type Session struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
}

func userFromContext(ctx context.Context) *Session {
	u, _ := ctx.Value(ctxUser).(*Session)
	return u
}

// ── Session helpers ───────────────────────────────────────────────────────────

func (s *Server) createSession(ctx context.Context, userID int, username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(raw)

	sess := Session{UserID: userID, Username: username}
	data, _ := json.Marshal(sess)

	key := "session:" + token
	if err := s.rdb.Set(ctx, key, data, sessionTTL).Err(); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return token, nil
}

func (s *Server) sessionFromRequest(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, nil // no cookie — not an error, just unauthenticated
	}

	key := "session:" + cookie.Value
	data, err := s.rdb.Get(r.Context(), key).Bytes()
	if err != nil {
		return nil, nil // expired or invalid
	}

	// Slide the TTL on activity so active users stay logged in.
	s.rdb.Expire(r.Context(), key, sessionTTL)

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &sess, nil
}

func (s *Server) deleteSession(ctx context.Context, token string) {
	s.rdb.Del(ctx, "session:"+token)
}

// ── Middleware ────────────────────────────────────────────────────────────────

// requireAuth wraps a handler and returns 401 if the request has no valid
// session. The Session is attached to the request context so downstream
// handlers can call userFromContext without touching Valkey again.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.sessionFromRequest(r)
		if err != nil {
			slog.Warn("session decode error", "error", err, "path", r.URL.Path)
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		if sess == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// POST /api/auth/register
// Controlled by ALLOW_REGISTRATION env var (default true). Set to false once
// all accounts are created to close the endpoint.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !AllowRegistration {
		http.Error(w, "registration is disabled", http.StatusForbidden)
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(payload.Username) < 2 || len(payload.Password) < 8 {
		http.Error(w, "username >= 2 chars, password >= 8 chars", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcryptCost)
	if err != nil {
		slog.Error("bcrypt failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	var id int
	err = s.db.QueryRow(r.Context(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		payload.Username, string(hash),
	).Scan(&id)
	if err != nil {
		slog.Warn("register: username already taken", "username", payload.Username)
		http.Error(w, "username already taken", http.StatusConflict)
		return
	}

	token, err := s.createSession(r.Context(), id, payload.Username)
	if err != nil {
		slog.Error("register: session creation failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	s.setSessionCookie(w, token)
	slog.Info("user registered", "username", payload.Username, "id", id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": id, "username": payload.Username})
}

// POST /api/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var id int
	var hash string
	err := s.db.QueryRow(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = $1`,
		payload.Username,
	).Scan(&id, &hash)
	if err != nil {
		// Always run bcrypt even on miss to prevent timing-based user enumeration.
		bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy"), []byte(payload.Password))
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(payload.Password)); err != nil {
		slog.Warn("login: bad password", "username", payload.Username)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := s.createSession(r.Context(), id, payload.Username)
	if err != nil {
		slog.Error("login: session creation failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	s.setSessionCookie(w, token)
	slog.Info("user logged in", "username", payload.Username, "id", id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "username": payload.Username})
}

// POST /api/auth/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		s.deleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookieName,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/auth/me
// Returns the current user or 401. Called by the frontend on load to decide
// whether to show the login screen or the main app.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil || sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":                 sess.UserID,
		"username":           sess.Username,
		"allow_registration": AllowRegistration,
	})
}

// ── Cookie helper ─────────────────────────────────────────────────────────────

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true, // requires HTTPS — standard on k8s ingress with TLS
		SameSite: http.SameSiteStrictMode,
	})
}
