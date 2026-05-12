package storage

import (
	"context"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("not found")

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
	s.urls[id] = url
	return nil
}

func (s *MemStorage) SaveBatch(_ context.Context, items []BatchItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
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
