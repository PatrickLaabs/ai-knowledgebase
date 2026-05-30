package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const tagTreeCacheTTL = 30 * time.Second

func tagTreeCacheKey(userID int) string {
	return fmt.Sprintf("tagtree:%d", userID)
}

// cachedTagTree returns the tag tree for a user, using Valkey as a read-through
// cache with a 30-second TTL. The flat tag slice is cached (not the rendered
// tree) so buildTagTree still runs in Go — cheap, and avoids caching HTML.
func (s *Server) cachedTagTree(ctx context.Context, userID int) []*TagNode {
	key := tagTreeCacheKey(userID)

	// Cache hit
	if data, err := s.rdb.Get(ctx, key).Bytes(); err == nil {
		var tags []string
		if json.Unmarshal(data, &tags) == nil {
			return buildTagTree(tags)
		}
	}

	// Cache miss — query Postgres
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT unnest(tags) AS tag
		FROM notes WHERE user_id = $1 ORDER BY tag
	`, userID)
	if err != nil {
		slog.Warn("cachedTagTree: query failed", "user_id", userID, "error", err)
		return nil
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tags = append(tags, t)
		}
	}

	// Write to cache — fire-and-forget, never block the response on this
	if data, err := json.Marshal(tags); err == nil {
		if err := s.rdb.Set(ctx, key, data, tagTreeCacheTTL).Err(); err != nil {
			slog.Warn("cachedTagTree: cache write failed", "error", err)
		}
	}

	return buildTagTree(tags)
}

// invalidateTagTreeCache removes the cached tag tree for a user.
// Call this after any operation that changes tags (note create, update, delete).
func (s *Server) invalidateTagTreeCache(ctx context.Context, userID int) {
	if err := s.rdb.Del(ctx, tagTreeCacheKey(userID)).Err(); err != nil {
		slog.Warn("invalidateTagTreeCache: failed", "user_id", userID, "error", err)
	}
}
