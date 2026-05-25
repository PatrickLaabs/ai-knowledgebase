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

// contextNote is the lightweight representation sent to the UI sources panel.
type contextNote struct {
	ID      int      `json:"id"`
	Preview string   `json:"preview"`
	Tags    []string `json:"tags"`
}

// embedText calls the Ollama embedding model and returns the float32 vector.
func (s *Server) embedText(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	resp, err := s.ollama.Embed(ctx, &api.EmbedRequest{
		Model: EmbedModel,
		Input: text,
	})
	if err != nil {
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

// retrieveContext embeds the query and returns the top-k most similar notes
// scoped to the given userID, above the similarity threshold.
func (s *Server) retrieveContext(ctx context.Context, query string, k int, userID int) (string, []contextNote, error) {
	emb, err := s.embedText(ctx, query)
	if err != nil {
		return "", nil, err
	}
	vec := pgvector.NewVector(emb)

	rows, err := s.db.Query(ctx, `
		SELECT id, content, tags, 1 - (embedding <=> $1) AS similarity
		FROM   notes
		WHERE  embedding IS NOT NULL
		  AND  user_id = $2
		  AND  1 - (embedding <=> $1) > 0.4
		ORDER  BY embedding <=> $1
		LIMIT  $3
	`, vec, userID, k)
	if err != nil {
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
		fmt.Fprintf(&prompt, "[Note #%d | tags: %s | similarity: %.2f]\n%s\n\n",
			id, strings.Join(tags, ", "), sim, content)

		preview := content
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		previews = append(previews, contextNote{ID: id, Preview: preview, Tags: tags})
	}

	slog.Info("context retrieved",
		"user_id", userID,
		"notes_found", len(previews),
	)
	return prompt.String(), previews, nil
}
