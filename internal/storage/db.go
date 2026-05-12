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

func (s *DBStorage) Save(ctx context.Context, id, url string) error {
	var stored string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO short_urls (short_url, original_url) VALUES ($1, $2)
		 ON CONFLICT (original_url) DO NOTHING
		 RETURNING short_url`,
		id, url).Scan(&stored)
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

func (s *DBStorage) SaveBatch(ctx context.Context, items []BatchItem) error {
	placeholders := make([]string, 0, len(items))
	args := make([]interface{}, 0, 2*len(items))
	for i, it := range items {
		placeholders = append(placeholders,
			"($"+strconv.Itoa(2*i+1)+", $"+strconv.Itoa(2*i+2)+")")
		args = append(args, it.ID, it.URL)
	}
	query := "INSERT INTO short_urls (short_url, original_url) VALUES " +
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
