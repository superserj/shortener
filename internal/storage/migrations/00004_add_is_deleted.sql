-- +goose Up
ALTER TABLE short_urls ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE short_urls DROP COLUMN IF EXISTS is_deleted;
