package storage

import (
	"context"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("not found")

type ConflictError struct {
	ShortURL string
}

func (e *ConflictError) Error() string {
	return "url already shortened: " + e.ShortURL
}

type BatchItem struct {
	ID  string
	URL string
}

type UserURL struct {
	ShortURL    string
	OriginalURL string
}

type Repository interface {
	Save(ctx context.Context, id, url, userID string) error
	SaveBatch(ctx context.Context, items []BatchItem, userID string) error
	Get(ctx context.Context, id string) (string, error)
	ListByUser(ctx context.Context, userID string) ([]UserURL, error)
}

type record struct {
	url    string
	userID string
}

type MemStorage struct {
	mu   sync.RWMutex
	urls map[string]record
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		urls: make(map[string]record),
	}
}

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

func (s *MemStorage) Find(url string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findByURL(url)
}

func (s *MemStorage) SaveBatch(_ context.Context, items []BatchItem, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		if _, ok := s.findByURL(it.URL); ok {
			continue
		}
		s.urls[it.ID] = record{url: it.URL, userID: userID}
	}
	return nil
}

func (s *MemStorage) Get(_ context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.urls[id]
	if !ok {
		return "", ErrNotFound
	}
	return rec.url, nil
}

func (s *MemStorage) ListByUser(_ context.Context, userID string) ([]UserURL, error) {
	if userID == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []UserURL
	for id, rec := range s.urls {
		if rec.userID == userID {
			result = append(result, UserURL{ShortURL: id, OriginalURL: rec.url})
		}
	}
	return result, nil
}
