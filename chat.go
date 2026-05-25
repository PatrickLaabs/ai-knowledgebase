package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

// POST /api/chat  ->  SSE stream
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	var payload struct {
		Query     string `json:"query"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	if payload.SessionID == "" {
		payload.SessionID = "default"
	}

	// Chat history is scoped per user so conversations never bleed across accounts.
	valkeyKey := fmt.Sprintf("chat_history:%d:%s", user.UserID, payload.SessionID)

	// ── 1. Load conversation history ──────────────────────────────────────────
	history, err := s.rdb.LRange(r.Context(), valkeyKey, 0, 9).Result()
	if err != nil {
		slog.Warn("failed to fetch chat history", "user_id", user.UserID, "error", err)
	}

	// ── 2. Retrieve relevant notes for this user via vector similarity ─────────
	contextText, sources, err := s.retrieveContext(r.Context(), payload.Query, 5, user.UserID)
	if err != nil {
		slog.Warn("context retrieval failed", "user_id", user.UserID, "error", err)
	}

	// Reverse history from newest-first to chronological for the prompt.
	var historyBuilder strings.Builder
	for i := len(history) - 1; i >= 0; i-- {
		historyBuilder.WriteString(history[i] + "\n")
	}

	slog.Info("chat request",
		"user", user.Username,
		"query_len", len(payload.Query),
		"context_notes", len(sources),
		"history_turns", len(history),
	)

	// ── 3. Build system prompt ────────────────────────────────────────────────
	systemPrompt := "You are a helpful assistant for a personal knowledge base. " +
		"Use the provided notes to answer questions. Be concise and accurate. " +
		"If the notes don't contain relevant information, say so clearly.\n\n" +
		"Relevant notes:\n" + contextText + "\n" +
		"Recent conversation:\n" + historyBuilder.String()

	// ── 4. SSE setup ──────────────────────────────────────────────────────────
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if sources == nil {
		sources = []contextNote{}
	}
	sourcesPayload, _ := json.Marshal(map[string]any{"sources": sources})
	fmt.Fprintf(w, "data: %s\n\n", sourcesPayload)
	flusher.Flush()

	// ── 5. Stream LLM response ────────────────────────────────────────────────
	genStart := time.Now()
	var fullResponse strings.Builder

	err = s.ollama.Generate(r.Context(), &api.GenerateRequest{
		Model:  LLMModel,
		System: systemPrompt,
		Prompt: payload.Query,
		Stream: new(true),
	}, func(resp api.GenerateResponse) error {
		fullResponse.WriteString(resp.Response)
		chunk, _ := json.Marshal(map[string]any{"chunk": resp.Response, "done": resp.Done})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
		return nil
	})

	if err != nil {
		slog.Error("LLM generation failed", "user", user.Username, "error", err)
		errPayload, _ := json.Marshal(map[string]any{"error": err.Error(), "done": true})
		fmt.Fprintf(w, "data: %s\n\n", errPayload)
		flusher.Flush()
		return
	}

	// ── 6. Persist turn to Valkey ─────────────────────────────────────────────
	s.rdb.LPush(r.Context(), valkeyKey,
		"Assistant: "+fullResponse.String(),
		"User: "+payload.Query,
	)
	s.rdb.LTrim(r.Context(), valkeyKey, 0, 9)
	s.rdb.Expire(r.Context(), valkeyKey, 30*time.Minute)

	slog.Info("chat complete",
		"user", user.Username,
		"context_notes", len(sources),
		"response_len", fullResponse.Len(),
		"duration_ms", time.Since(genStart).Milliseconds(),
	)
}
