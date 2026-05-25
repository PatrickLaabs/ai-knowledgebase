-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS users (
    id            SERIAL PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Scope every note to its owner. Nullable so existing notes survive the
-- migration — the application always sets user_id on new writes.
ALTER TABLE notes
    ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS notes_user_id_idx ON notes (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE notes DROP COLUMN IF EXISTS user_id;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
