package storage

import "sync"

type Repository interface {
	Save(id, url string)
	Get(id string) (string, bool)
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

func (s *MemStorage) Save(id, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls[id] = url
}

func (s *MemStorage) Get(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.urls[id]
	return url, ok
}
