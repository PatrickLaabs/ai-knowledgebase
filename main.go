package main

import (
	"context"
	"io/fs"
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
		"allow_registration", AllowRegistration,
	)

	ctx := context.Background()

	// ── Ollama ────────────────────────────────────────────────────────────────
	ollamaClient, err := api.ClientFromEnvironment()
	if err != nil {
		slog.Error("failed to create Ollama client", "error", err)
		os.Exit(1)
	}

	// ── Valkey ────────────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{Addr: ValkeyUrl})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to valkey", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to valkey")

	// ── Goose migrations ──────────────────────────────────────────────────────
	migrationDB, err := goose.OpenDBWithDriver("pgx", DBUrl)
	if err != nil {
		slog.Error("failed to open migration db", "error", err)
		os.Exit(1)
	}
	goose.SetDialect("postgres")
	goose.SetBaseFS(embedMigrations)
	slog.Info("running migrations…")
	if err := goose.Up(migrationDB, "migrations"); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	migrationDB.Close()

	// ── Postgres pool ─────────────────────────────────────────────────────────
	poolCfg, err := pgxpool.ParseConfig(DBUrl)
	if err != nil {
		slog.Error("invalid POSTGRES_URI", "error", err)
		os.Exit(1)
	}
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		slog.Error("failed to create db pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
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

	// ── Static assets ─────────────────────────────────────────────────────────
	webFS, err := fs.Sub(embedWeb, "web")
	if err != nil {
		slog.Error("failed to sub web FS", "error", err)
		os.Exit(1)
	}

	// ── Server ────────────────────────────────────────────────────────────────
	srv := &Server{ollama: ollamaClient, db: pool, rdb: rdb}
	mux := http.NewServeMux()

	// Helper to wrap a handler with requireAuth cleanly.
	auth := func(h http.HandlerFunc) http.Handler { return srv.requireAuth(h) }

	// Static files + SPA shell
	mux.Handle("GET /static/", http.FileServerFS(webFS))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, webFS, "index.html")
	})

	// Public — no auth required
	mux.HandleFunc("GET /api/health", srv.handleHealth)
	mux.HandleFunc("GET /api/auth/me", srv.handleMe)
	mux.HandleFunc("POST /api/auth/login", srv.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", srv.handleLogout)
	mux.HandleFunc("POST /api/auth/register", srv.handleRegister)

	// Protected — valid session required
	mux.Handle("GET /api/notes", auth(srv.handleListNotes))
	mux.Handle("POST /api/notes", auth(srv.handleCreateNote))
	mux.Handle("PUT /api/notes/{id}", auth(srv.handleUpdateNote))
	mux.Handle("DELETE /api/notes/{id}", auth(srv.handleDeleteNote))
	mux.Handle("GET /api/tags", auth(srv.handleListTags))
	mux.Handle("POST /api/chat", auth(srv.handleChat))
	mux.Handle("POST /api/admin/reindex", auth(srv.handleStartReindex))
	mux.Handle("GET /api/admin/reindex/status", auth(srv.handleReindexStatus))

	slog.Info("server ready", "addr", ListenAddr)
	if err := http.ListenAndServe(ListenAddr, loggingMiddleware(mux)); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}
