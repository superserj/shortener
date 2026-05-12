package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"

	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/logger"
)

type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

type FileStorage struct {
	mu      sync.Mutex
	mem     *MemStorage
	file    *os.File
	encoder *json.Encoder
	nextID  int
}

func NewFileStorage(path string) (*FileStorage, error) {
	mem := NewMemStorage()

	nextID, err := loadRecords(path, mem)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	return &FileStorage{
		mem:     mem,
		file:    file,
		encoder: json.NewEncoder(file),
		nextID:  nextID,
	}, nil
}

func loadRecords(path string, mem *MemStorage) (int, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	nextID := 0
	for {
		var rec Record
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		_ = mem.Save(context.Background(), rec.ShortURL, rec.OriginalURL)
		if n, convErr := strconv.Atoi(rec.UUID); convErr == nil && n > nextID {
			nextID = n
		}
	}
	return nextID, nil
}

func (s *FileStorage) Save(ctx context.Context, id, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := &Record{
		UUID:        strconv.Itoa(s.nextID + 1),
		ShortURL:    id,
		OriginalURL: url,
	}
	if err := s.encoder.Encode(rec); err != nil {
		logger.Log.Warn("failed to persist record", zap.Error(err))
		return err
	}
	s.nextID++
	return s.mem.Save(ctx, id, url)
}

func (s *FileStorage) Get(ctx context.Context, id string) (string, error) {
	return s.mem.Get(ctx, id)
}

func (s *FileStorage) Close() error {
	return s.file.Close()
}
