-- +goose Up
CREATE UNIQUE INDEX short_urls_original_url_uniq ON short_urls (original_url);

-- +goose Down
DROP INDEX IF EXISTS short_urls_original_url_uniq;
