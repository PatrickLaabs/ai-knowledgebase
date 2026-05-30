-- +goose Up
-- Composite index on (user_id, updated_at DESC) — covers the default notes
-- list query ORDER BY updated_at DESC and all WHERE user_id = $1 filters.
CREATE INDEX IF NOT EXISTS idx_notes_user_updated
    ON notes (user_id, updated_at DESC);

-- Index for the tag unnest existence check used in tag-filtered queries.
CREATE INDEX IF NOT EXISTS idx_notes_user_tags
    ON notes USING GIN (tags)
    WHERE user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_notes_user_updated;
DROP INDEX IF EXISTS idx_notes_user_tags;
