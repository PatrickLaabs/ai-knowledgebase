package main

import (
	"embed"
	"os"
)

// getEnv returns the value of key from the environment, or fallback if unset/empty.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

var (
	ListenAddr     = getEnv("LISTEN_ADDR", ":8080")
	LLMModel       = getEnv("LLM_MODEL", "granite4.1:3b")
	EmbedModel     = getEnv("EMBED_MODEL", "snowflake-arctic-embed2:568m")
	EmbedDimension = getEnv("EMBEDDING_DIMENSION", "768")
	DBUrl          = getEnv("POSTGRES_URI", "localhost:5432")
	ValkeyUrl      = getEnv("VALKEY_URI", "localhost:6379")

	//go:embed migrations/*.sql
	embedMigrations embed.FS
)
