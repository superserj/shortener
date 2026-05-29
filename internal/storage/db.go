package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DBStorage struct {
	pool *pgxpool.Pool
}

func NewDBStorage(ctx context.Context, dsn string) (*DBStorage, error) {
	if err := migrate(dsn); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &DBStorage{pool: pool}, nil
}

func migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

func (s *DBStorage) Close() error {
	s.pool.Close()
	return nil
}

func (s *DBStorage) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *DBStorage) Save(ctx context.Context, id, url, userID string) error {
	var stored string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO short_urls (short_url, original_url, user_id) VALUES ($1, $2, $3)
		 ON CONFLICT (original_url) DO NOTHING
		 RETURNING short_url`,
		id, url, nullableUserID(userID)).Scan(&stored)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT short_url FROM short_urls WHERE original_url = $1`, url).
		Scan(&stored); err != nil {
		return err
	}
	return &ConflictError{ShortURL: stored}
}

func (s *DBStorage) SaveBatch(ctx context.Context, items []BatchItem, userID string) error {
	if len(items) == 0 {
		return nil
	}
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
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *DBStorage) Get(ctx context.Context, id string) (string, error) {
	var (
		original  string
		isDeleted bool
	)
	err := s.pool.QueryRow(ctx,
		`SELECT original_url, is_deleted FROM short_urls WHERE short_url = $1`, id).
		Scan(&original, &isDeleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if isDeleted {
		return "", ErrDeleted
	}
	return original, nil
}

func (s *DBStorage) ListByUser(ctx context.Context, userID string) ([]UserURL, error) {
	if userID == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT short_url, original_url FROM short_urls WHERE user_id = $1 AND is_deleted = FALSE`, userID)
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

func (s *DBStorage) MarkDeleted(ctx context.Context, userID string, ids []string) error {
	if userID == "" || len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE short_urls SET is_deleted = TRUE
		 WHERE user_id = $1 AND short_url = ANY($2) AND is_deleted = FALSE`,
		userID, ids)
	return err
}

func nullableUserID(userID string) interface{} {
	if userID == "" {
		return nil
	}
	return userID
}
