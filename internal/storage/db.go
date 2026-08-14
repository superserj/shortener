package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DBStorage хранит ссылки в PostgreSQL. Схема разворачивается миграциями
// при создании хранилища.
type DBStorage struct {
	pool *pgxpool.Pool
}

// NewDBStorage открывает пул соединений и накатывает миграции.
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

// Close закрывает пул соединений.
func (s *DBStorage) Close() error {
	s.pool.Close()
	return nil
}

// Ping проверяет соединение с базой данных.
func (s *DBStorage) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Save сохраняет ссылку. Если адрес уже сокращали, возвращает ConflictError.
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

// SaveBatch сохраняет пачку ссылок и возвращает ссылки, под которыми адреса
// лежат в базе. Для уже известного адреса это выданная ранее короткая ссылка:
// конфликтующие строки RETURNING не отдаёт, поэтому их адреса добираются
// отдельным запросом — так вставка не берёт блокировки на чужих строках.
func (s *DBStorage) SaveBatch(ctx context.Context, items []BatchItem, userID string) ([]BatchItem, error) {
	if len(items) == 0 {
		return nil, nil
	}

	// повторы внутри пачки отсеиваем: вставлять один адрес дважды в одном
	// запросе бессмысленно, а ссылку для повтора всё равно вернёт общий разбор
	unique := make([]BatchItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		if _, ok := seen[it.URL]; ok {
			continue
		}
		seen[it.URL] = struct{}{}
		unique = append(unique, it)
	}

	placeholders := make([]string, 0, len(unique))
	args := make([]any, 0, 3*len(unique))
	user := nullableUserID(userID)
	for i, it := range unique {
		base := 3 * i
		placeholders = append(placeholders,
			"($"+strconv.Itoa(base+1)+", $"+strconv.Itoa(base+2)+", $"+strconv.Itoa(base+3)+")")
		args = append(args, it.ID, it.URL, user)
	}
	query := "INSERT INTO short_urls (short_url, original_url, user_id) VALUES " +
		strings.Join(placeholders, ", ") +
		" ON CONFLICT (original_url) DO NOTHING RETURNING short_url, original_url"

	stored := make(map[string]string, len(unique))
	if err := s.collectShortURLs(ctx, stored, query, args...); err != nil {
		return nil, err
	}

	// адреса, на которых вставка споткнулась о конфликт, в RETURNING не попали:
	// их короткие ссылки читаем из базы
	known := make([]string, 0, len(unique))
	for _, it := range unique {
		if _, ok := stored[it.URL]; !ok {
			known = append(known, it.URL)
		}
	}
	if len(known) > 0 {
		if err := s.collectShortURLs(ctx, stored,
			`SELECT short_url, original_url FROM short_urls WHERE original_url = ANY($1)`, known); err != nil {
			return nil, err
		}
	}

	saved := make([]BatchItem, 0, len(items))
	for _, it := range items {
		short, ok := stored[it.URL]
		if !ok {
			return nil, fmt.Errorf("short url for %s is missing after batch insert", it.URL)
		}
		saved = append(saved, BatchItem{ID: short, URL: it.URL})
	}
	return saved, nil
}

// collectShortURLs выполняет запрос, возвращающий пары «короткая ссылка —
// оригинальный адрес», и складывает их в dst.
func (s *DBStorage) collectShortURLs(ctx context.Context, dst map[string]string, query string, args ...any) error {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var short, original string
		if err := rows.Scan(&short, &original); err != nil {
			return err
		}
		dst[original] = short
	}
	return rows.Err()
}

// Get возвращает оригинальный адрес по короткой ссылке. Для удалённой ссылки
// возвращает ErrDeleted, для неизвестной — ErrNotFound.
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

// ListByUser возвращает ссылки пользователя, кроме удалённых.
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

// MarkDeleted помечает удалёнными ссылки, принадлежащие пользователю.
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

func nullableUserID(userID string) any {
	if userID == "" {
		return nil
	}
	return userID
}
