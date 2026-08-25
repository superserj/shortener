// Пакет storage хранит соответствие коротких ссылок оригинальным адресам.
// Доступны три реализации Repository: в памяти, в файле и в PostgreSQL.
package storage

import (
	"context"
	"errors"
	"sync"
)

// Ошибки, по которым обработчики различают состояние ссылки.
var (
	// ErrNotFound возвращается, если короткой ссылки нет в хранилище.
	ErrNotFound = errors.New("not found")
	// ErrDeleted возвращается, если ссылка была удалена владельцем.
	ErrDeleted = errors.New("deleted")
)

// ConflictError сообщает, что оригинальный адрес уже сокращали, и несёт
// выданную ранее короткую ссылку.
type ConflictError struct {
	ShortURL string
}

// Error реализует интерфейс error.
func (e *ConflictError) Error() string {
	return "url already shortened: " + e.ShortURL
}

// BatchItem — одна пара «короткая ссылка — адрес» для пакетного сохранения.
type BatchItem struct {
	ID  string
	URL string
}

// UserURL — ссылка, созданная пользователем.
type UserURL struct {
	ShortURL    string
	OriginalURL string
}

// Repository — хранилище коротких ссылок.
type Repository interface {
	// Save сохраняет ссылку. Если адрес уже сокращали, возвращает ConflictError.
	Save(ctx context.Context, id, url, userID string) error
	// SaveBatch сохраняет пачку ссылок за одну операцию и возвращает ссылки,
	// под которыми адреса лежат в хранилище: для адреса, который уже сокращали,
	// это выданная ранее короткая ссылка, а не переданная в items. Результат
	// повторяет порядок и длину items, поэтому вызывающий код сопоставляет его
	// с исходной пачкой по индексу.
	SaveBatch(ctx context.Context, items []BatchItem, userID string) ([]BatchItem, error)
	// Get возвращает оригинальный адрес по короткой ссылке.
	Get(ctx context.Context, id string) (string, error)
	// ListByUser возвращает ссылки пользователя, кроме удалённых.
	ListByUser(ctx context.Context, userID string) ([]UserURL, error)
	// MarkDeleted помечает ссылки пользователя удалёнными.
	MarkDeleted(ctx context.Context, userID string, ids []string) error
}

type record struct {
	url     string
	userID  string
	deleted bool
}

// MemStorage хранит ссылки в памяти процесса и теряет их при перезапуске.
type MemStorage struct {
	mu   sync.RWMutex
	urls map[string]record
}

// NewMemStorage создаёт пустое хранилище в памяти.
func NewMemStorage() *MemStorage {
	return &MemStorage{
		urls: make(map[string]record),
	}
}

// Save сохраняет ссылку в памяти.
func (s *MemStorage) Save(_ context.Context, id, url, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.findByURL(url); ok {
		return &ConflictError{ShortURL: existing}
	}
	s.urls[id] = record{url: url, userID: userID}
	return nil
}

func (s *MemStorage) findByURL(url string) (string, bool) {
	for id, rec := range s.urls {
		if rec.url == url {
			return id, true
		}
	}
	return "", false
}

// Find ищет короткую ссылку по оригинальному адресу.
func (s *MemStorage) Find(url string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findByURL(url)
}

// SaveBatch сохраняет пачку ссылок. Для уже известного адреса возвращает
// выданную ранее короткую ссылку, в том числе когда адрес повторяется внутри
// самой пачки: повтор находится в хранилище сразу после сохранения первой копии.
func (s *MemStorage) SaveBatch(_ context.Context, items []BatchItem, userID string) ([]BatchItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved := make([]BatchItem, 0, len(items))
	for _, it := range items {
		if existing, ok := s.findByURL(it.URL); ok {
			saved = append(saved, BatchItem{ID: existing, URL: it.URL})
			continue
		}
		s.urls[it.ID] = record{url: it.URL, userID: userID}
		saved = append(saved, it)
	}
	return saved, nil
}

// Get возвращает оригинальный адрес по короткой ссылке. Для удалённой ссылки
// возвращает ErrDeleted, для неизвестной — ErrNotFound.
func (s *MemStorage) Get(_ context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.urls[id]
	if !ok {
		return "", ErrNotFound
	}
	if rec.deleted {
		return "", ErrDeleted
	}
	return rec.url, nil
}

// ListByUser возвращает ссылки пользователя, кроме удалённых.
func (s *MemStorage) ListByUser(_ context.Context, userID string) ([]UserURL, error) {
	if userID == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// считаем заранее, чтобы слайс не рос переаллокациями с копированием
	n := 0
	for _, rec := range s.urls {
		if rec.userID == userID && !rec.deleted {
			n++
		}
	}
	if n == 0 {
		return nil, nil
	}

	result := make([]UserURL, 0, n)
	for id, rec := range s.urls {
		if rec.userID == userID && !rec.deleted {
			result = append(result, UserURL{ShortURL: id, OriginalURL: rec.url})
		}
	}
	return result, nil
}

// MarkDeleted помечает удалёнными ссылки, принадлежащие пользователю. Чужие
// ссылки пропускает.
func (s *MemStorage) MarkDeleted(_ context.Context, userID string, ids []string) error {
	if userID == "" || len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		rec, ok := s.urls[id]
		if !ok || rec.userID != userID {
			continue
		}
		rec.deleted = true
		s.urls[id] = rec
	}
	return nil
}
