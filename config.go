package main

import (
	"embed"
	"os"
	"strconv"
)

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

var (
	ListenAddr        = getEnv("LISTEN_ADDR", ":8080")
	LLMModel          = getEnv("LLM_MODEL", "granite4.1:3b")
	EmbedModel        = getEnv("EMBED_MODEL", "snowflake-arctic-embed2:568m")
	EmbedDimension    = getEnv("EMBEDDING_DIMENSION", "768")
	DBUrl             = getEnv("POSTGRES_URI", "localhost:5432")
	ValkeyUrl         = getEnv("VALKEY_URI", "localhost:6379")
	AllowRegistration = getEnvBool("ALLOW_REGISTRATION", false)

	//go:embed migrations/*.sql
	embedMigrations embed.FS

	//go:embed web
	embedWeb embed.FS
)
