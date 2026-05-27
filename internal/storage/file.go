package storage

import (
	"bytes"
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
	UserID      string `json:"user_id,omitempty"`
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
		_ = mem.Save(context.Background(), rec.ShortURL, rec.OriginalURL, rec.UserID)
		if n, convErr := strconv.Atoi(rec.UUID); convErr == nil && n > nextID {
			nextID = n
		}
	}
	return nextID, nil
}

func (s *FileStorage) Save(ctx context.Context, id, url, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.mem.Find(url); ok {
		return &ConflictError{ShortURL: existing}
	}
	rec := &Record{
		UUID:        strconv.Itoa(s.nextID + 1),
		ShortURL:    id,
		OriginalURL: url,
		UserID:      userID,
	}
	if err := s.encoder.Encode(rec); err != nil {
		logger.Log.Warn("failed to persist record", zap.Error(err))
		return err
	}
	s.nextID++
	return s.mem.Save(ctx, id, url, userID)
}

func (s *FileStorage) SaveBatch(ctx context.Context, items []BatchItem, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, it := range items {
		rec := &Record{
			UUID:        strconv.Itoa(s.nextID + i + 1),
			ShortURL:    it.ID,
			OriginalURL: it.URL,
			UserID:      userID,
		}
		if err := enc.Encode(rec); err != nil {
			logger.Log.Warn("failed to encode batch record", zap.Error(err))
			return err
		}
	}
	if _, err := s.file.Write(buf.Bytes()); err != nil {
		logger.Log.Warn("failed to persist batch", zap.Error(err))
		return err
	}
	s.nextID += len(items)
	return s.mem.SaveBatch(ctx, items, userID)
}

func (s *FileStorage) Get(ctx context.Context, id string) (string, error) {
	return s.mem.Get(ctx, id)
}

func (s *FileStorage) ListByUser(ctx context.Context, userID string) ([]UserURL, error) {
	return s.mem.ListByUser(ctx, userID)
}

func (s *FileStorage) Close() error {
	return s.file.Close()
}
