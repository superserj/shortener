-- +goose Up
CREATE TABLE IF NOT EXISTS short_urls (
    short_url    TEXT PRIMARY KEY,
    original_url TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS short_urls;
