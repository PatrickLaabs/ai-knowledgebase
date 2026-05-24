package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ollama/ollama/api"
	"github.com/redis/go-redis/v9"
)

// Server holds the shared dependencies injected at startup.
// All HTTP handlers are methods on this type.
type Server struct {
	ollama *api.Client
	db     *pgxpool.Pool
	rdb    *redis.Client
}

// ensureVectorSchema checks whether the embedding column dimension and HNSW
// index match the configured dimension, and fixes them if not. Safe to call
// on every startup — it is a no-op when everything is already correct.
//
// This lives outside goose migrations intentionally: the dimension is a
// runtime config value (EMBEDDING_DIMENSION env var) that can change when
// switching embedding models, so it cannot be baked into a static SQL file.
func ensureVectorSchema(ctx context.Context, pool *pgxpool.Pool, dim int) error {
	// pg_attribute.atttypmod encodes the vector dimension as (dim + 4).
	// A value of -1 means the column is dimensionless (bare `vector`).
	var atttypmod int
	err := pool.QueryRow(ctx, `
		SELECT atttypmod
		FROM   pg_attribute
		JOIN   pg_class ON attrelid = pg_class.oid
		WHERE  relname = 'notes' AND attname = 'embedding'
	`).Scan(&atttypmod)
	if err != nil {
		return fmt.Errorf("check embedding column: %w", err)
	}

	currentDim := atttypmod - 4 // pgvector encoding
	if currentDim == dim {
		slog.Info("embedding column dimension already correct", "dim", dim)
		return nil
	}

	slog.Info("updating embedding column dimension",
		"current_dim", currentDim,
		"target_dim", dim,
	)

	// Drop the existing index first — ALTER TYPE fails while the index exists.
	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS notes_embedding_idx`); err != nil {
		return fmt.Errorf("drop embedding index: %w", err)
	}

	// Warn loudly when changing an existing dimension — stored embeddings
	// become invalid and a re-index will be required.
	if currentDim > 0 {
		slog.Warn("embedding dimension changed — existing embeddings are now invalid, run a re-index",
			"old_dim", currentDim,
			"new_dim", dim,
		)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE notes ALTER COLUMN embedding TYPE vector(%d)`, dim,
	)); err != nil {
		return fmt.Errorf("alter embedding column to vector(%d): %w", dim, err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE INDEX notes_embedding_idx
		ON     notes
		USING  hnsw (embedding vector_cosine_ops)
		WITH   (m = 16, ef_construction = 64)
	`); err != nil {
		return fmt.Errorf("create hnsw index: %w", err)
	}

	slog.Info("embedding schema updated", "dim", dim)
	return nil
}
