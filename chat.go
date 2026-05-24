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

// POST /api/chat  →  SSE stream
//
// Flow:
//  1. Embed the query and retrieve the top-k relevant notes from pgvector
//  2. Load the last N conversation turns from Valkey
//  3. Build a system prompt combining retrieved context + history
//  4. Stream the LLM response token-by-token over SSE
//  5. Persist the completed turn back to Valkey for future context
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Query     string `json:"query"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	if payload.SessionID == "" {
		payload.SessionID = "default-session"
	}
	valkeyKey := "chat_history:" + payload.SessionID

	// ── 1. Fetch conversation history from Valkey ─────────────────────────────
	history, err := s.rdb.LRange(r.Context(), valkeyKey, 0, 9).Result()
	if err != nil {
		slog.Warn("failed to fetch history from valkey", "error", err)
	}

	// ── 2. Retrieve relevant notes via vector similarity ──────────────────────
	contextText, sources, err := s.retrieveContext(r.Context(), payload.Query, 5)
	if err != nil {
		slog.Warn("context retrieval failed, continuing without note context", "error", err)
	}

	// History is stored newest-first (LPush); reverse to chronological order.
	var historyBuilder strings.Builder
	for i := len(history) - 1; i >= 0; i-- {
		historyBuilder.WriteString(history[i] + "\n")
	}

	slog.Info("chat request received",
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

	// Send source notes to the UI before the first token arrives.
	if sources == nil {
		sources = []contextNote{}
	}
	sourcesPayload, _ := json.Marshal(map[string]any{"sources": sources})
	fmt.Fprintf(w, "data: %s\n\n", sourcesPayload)
	flusher.Flush()

	// ── 5. Stream LLM response ────────────────────────────────────────────────
	genStart := time.Now()
	var totalChunks, totalLen int
	var fullResponse strings.Builder

	err = s.ollama.Generate(r.Context(), &api.GenerateRequest{
		Model:  LLMModel,
		System: systemPrompt,
		Prompt: payload.Query,
		Stream: new(true),
	}, func(resp api.GenerateResponse) error {
		fullResponse.WriteString(resp.Response)
		totalChunks++
		totalLen += len(resp.Response)

		chunk, _ := json.Marshal(map[string]any{
			"chunk": resp.Response,
			"done":  resp.Done,
		})
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
		return nil
	})

	duration := time.Since(genStart)

	if err != nil {
		slog.Error("LLM generation failed",
			"model", LLMModel,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		errPayload, _ := json.Marshal(map[string]any{"error": err.Error(), "done": true})
		fmt.Fprintf(w, "data: %s\n\n", errPayload)
		flusher.Flush()
		return
	}

	// ── 6. Persist turn to Valkey ─────────────────────────────────────────────
	userTurn := "User: " + payload.Query
	aiTurn := "Assistant: " + fullResponse.String()
	s.rdb.LPush(r.Context(), valkeyKey, aiTurn, userTurn) // newest-first
	s.rdb.LTrim(r.Context(), valkeyKey, 0, 9)            // keep last 5 turns (10 entries)
	s.rdb.Expire(r.Context(), valkeyKey, 30*time.Minute)

	slog.Info("chat complete",
		"model", LLMModel,
		"context_notes", len(sources),
		"response_len", totalLen,
		"chunks", totalChunks,
		"duration_ms", duration.Milliseconds(),
	)
}
