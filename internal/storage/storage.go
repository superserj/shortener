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

type Repository interface {
	Save(ctx context.Context, id, url string) error
	SaveBatch(ctx context.Context, items []BatchItem) error
	Get(ctx context.Context, id string) (string, error)
}

type MemStorage struct {
	mu   sync.RWMutex
	urls map[string]string
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		urls: make(map[string]string),
	}
}

func (s *MemStorage) Save(_ context.Context, id, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.findByURL(url); ok {
		return &ConflictError{ShortURL: existing}
	}
	s.urls[id] = url
	return nil
}

func (s *MemStorage) findByURL(url string) (string, bool) {
	for id, u := range s.urls {
		if u == url {
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

func (s *MemStorage) SaveBatch(_ context.Context, items []BatchItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		if _, ok := s.findByURL(it.URL); ok {
			continue
		}
		s.urls[it.ID] = it.URL
	}
	return nil
}

func (s *MemStorage) Get(_ context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.urls[id]
	if !ok {
		return "", ErrNotFound
	}
	return url, nil
}
