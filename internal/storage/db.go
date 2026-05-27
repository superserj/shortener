package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DBStorage struct {
	db *sql.DB
}

func NewDBStorage(db *sql.DB) (*DBStorage, error) {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return nil, err
	}
	return &DBStorage{db: db}, nil
}

func (s *DBStorage) Save(ctx context.Context, id, url, userID string) error {
	var stored string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO short_urls (short_url, original_url, user_id) VALUES ($1, $2, $3)
		 ON CONFLICT (original_url) DO NOTHING
		 RETURNING short_url`,
		id, url, nullableUserID(userID)).Scan(&stored)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT short_url FROM short_urls WHERE original_url = $1`, url).
		Scan(&stored); err != nil {
		return err
	}
	return &ConflictError{ShortURL: stored}
}

func (s *DBStorage) SaveBatch(ctx context.Context, items []BatchItem, userID string) error {
	placeholders := make([]string, 0, len(items))
	args := make([]interface{}, 0, 3*len(items))
	user := nullableUserID(userID)
	for i, it := range items {
		base := 3 * i
		placeholders = append(placeholders,
			"($"+strconv.Itoa(base+1)+", $"+strconv.Itoa(base+2)+", $"+strconv.Itoa(base+3)+")")
		args = append(args, it.ID, it.URL, user)
	}
	query := "INSERT INTO short_urls (short_url, original_url, user_id) VALUES " +
		strings.Join(placeholders, ", ") +
		" ON CONFLICT (original_url) DO NOTHING"
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *DBStorage) Get(ctx context.Context, id string) (string, error) {
	var original string
	err := s.db.QueryRowContext(ctx,
		`SELECT original_url FROM short_urls WHERE short_url = $1`, id).
		Scan(&original)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return original, nil
}

func (s *DBStorage) ListByUser(ctx context.Context, userID string) ([]UserURL, error) {
	if userID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT short_url, original_url FROM short_urls WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserURL
	for rows.Next() {
		var u UserURL
		if err := rows.Scan(&u.ShortURL, &u.OriginalURL); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func nullableUserID(userID string) interface{} {
	if userID == "" {
		return nil
	}
	return userID
}
