package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ollama/ollama/api"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	slog.Info("starting knowledge base",
		"listen_addr", ListenAddr,
		"llm_model", LLMModel,
		"embed_model", EmbedModel,
		"embed_dimension", EmbedDimension,
	)

	ctx := context.Background()

	// ── Ollama ────────────────────────────────────────────────────────────────
	ollamaClient, err := api.ClientFromEnvironment()
	if err != nil {
		slog.Error("failed to create Ollama client", "error", err)
		os.Exit(1)
	}
	slog.Info("ollama client ready")

	// ── Valkey ────────────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     ValkeyUrl,
		Password: "",
		DB:       0,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to valkey", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to valkey")

	// ── Goose migrations ──────────────────────────────────────────────────────
	// Goose requires a standard *sql.DB; we open a separate short-lived
	// connection just for migrations, then close it before creating the pool.
	migrationDB, err := goose.OpenDBWithDriver("pgx", DBUrl)
	if err != nil {
		slog.Error("failed to open migration db connection", "error", err)
		os.Exit(1)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		slog.Error("failed to set migration dialect", "error", err)
		os.Exit(1)
	}
	goose.SetBaseFS(embedMigrations)
	slog.Info("checking database migration state…")
	if err := goose.Up(migrationDB, "migrations"); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	if err := migrationDB.Close(); err != nil {
		slog.Error("failed to close migration db", "error", err)
		os.Exit(1)
	}

	// ── Postgres pool ─────────────────────────────────────────────────────────
	poolCfg, err := pgxpool.ParseConfig(DBUrl)
	if err != nil {
		slog.Error("invalid POSTGRES_URI", "error", err)
		os.Exit(1)
	}
	// Register pgvector types on every new connection in the pool.
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		slog.Error("failed to create db pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("failed to reach postgres", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to postgres")

	// ── Vector schema ─────────────────────────────────────────────────────────
	dim, err := strconv.Atoi(EmbedDimension)
	if err != nil || dim <= 0 {
		slog.Error("invalid EMBEDDING_DIMENSION", "value", EmbedDimension)
		os.Exit(1)
	}
	if err := ensureVectorSchema(ctx, pool, dim); err != nil {
		slog.Error("failed to ensure vector schema", "error", err)
		os.Exit(1)
	}

	// ── Server + routes ───────────────────────────────────────────────────────
	srv := &Server{ollama: ollamaClient, db: pool, rdb: rdb}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Health
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	// Notes
	mux.HandleFunc("GET /api/notes", srv.handleListNotes)
	mux.HandleFunc("POST /api/notes", srv.handleCreateNote)
	mux.HandleFunc("PUT /api/notes/{id}", srv.handleUpdateNote)
	mux.HandleFunc("DELETE /api/notes/{id}", srv.handleDeleteNote)
	mux.HandleFunc("GET /api/tags", srv.handleListTags)

	// Chat
	mux.HandleFunc("POST /api/chat", srv.handleChat)

	// Admin
	mux.HandleFunc("POST /api/admin/reindex", srv.handleStartReindex)
	mux.HandleFunc("GET /api/admin/reindex/status", srv.handleReindexStatus)

	slog.Info("routes registered, server ready", "addr", ListenAddr)
	if err := http.ListenAndServe(ListenAddr, loggingMiddleware(mux)); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}
