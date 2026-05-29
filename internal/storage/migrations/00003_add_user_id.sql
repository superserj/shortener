-- +goose Up
ALTER TABLE short_urls ADD COLUMN IF NOT EXISTS user_id TEXT;
CREATE INDEX IF NOT EXISTS short_urls_user_id_idx ON short_urls (user_id);

-- +goose Down
DROP INDEX IF EXISTS short_urls_user_id_idx;
ALTER TABLE short_urls DROP COLUMN IF EXISTS user_id;
