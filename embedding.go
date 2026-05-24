package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
	pgvector "github.com/pgvector/pgvector-go"
)

// contextNote is the lightweight representation sent to the UI so it can show
// which notes were used as context, without sending the full content over SSE.
type contextNote struct {
	ID      int      `json:"id"`
	Preview string   `json:"preview"`
	Tags    []string `json:"tags"`
}

// embedText calls the Ollama embedding model and returns the raw float32 vector.
// It logs timing and dimension so you can verify the model is behaving correctly.
func (s *Server) embedText(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	resp, err := s.ollama.Embed(ctx, &api.EmbedRequest{
		Model: EmbedModel,
		Input: text,
	})
	if err != nil {
		slog.Error("embed failed",
			"model", EmbedModel,
			"text_len", len(text),
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	slog.Debug("embed complete",
		"model", EmbedModel,
		"text_len", len(text),
		"dims", len(resp.Embeddings[0]),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return resp.Embeddings[0], nil
}

// retrieveContext embeds the query, runs a cosine similarity search against
// all indexed notes, and returns the top-k results above the similarity
// threshold. It returns both the formatted context block for the system prompt
// and lightweight previews for the UI sources panel.
func (s *Server) retrieveContext(ctx context.Context, query string, k int) (string, []contextNote, error) {
	emb, err := s.embedText(ctx, query)
	if err != nil {
		return "", nil, err
	}
	vec := pgvector.NewVector(emb)

	start := time.Now()
	rows, err := s.db.Query(ctx, `
		SELECT id, content, tags, 1 - (embedding <=> $1) AS similarity
		FROM   notes
		WHERE  embedding IS NOT NULL
		  AND  1 - (embedding <=> $1) > 0.4
		ORDER  BY embedding <=> $1
		LIMIT  $2
	`, vec, k)
	if err != nil {
		slog.Error("context retrieval query failed", "error", err)
		return "", nil, fmt.Errorf("retrieval query: %w", err)
	}
	defer rows.Close()

	var prompt strings.Builder
	var previews []contextNote

	for rows.Next() {
		var id int
		var content string
		var tags []string
		var sim float64
		if err := rows.Scan(&id, &content, &tags, &sim); err != nil {
			continue
		}

		tagStr := strings.Join(tags, ", ")
		fmt.Fprintf(&prompt, "[Note #%d | tags: %s | similarity: %.2f]\n%s\n\n", id, tagStr, sim, content)

		preview := content
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		previews = append(previews, contextNote{ID: id, Preview: preview, Tags: tags})
	}

	slog.Info("context retrieved",
		"query_len", len(query),
		"notes_found", len(previews),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return prompt.String(), previews, nil
}
