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
)

// Record — запись файлового хранилища, одна строка JSON в логе.
type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
	UserID      string `json:"user_id,omitempty"`
	IsDeleted   bool   `json:"is_deleted,omitempty"`
}

// FileStorage дописывает записи в файл и держит их копию в памяти.
type FileStorage struct {
	mu      sync.Mutex
	mem     *MemStorage
	file    *os.File
	encoder *json.Encoder
	nextID  int
	log     *zap.Logger
}

// NewFileStorage открывает файл хранилища, вычитывая уже сохранённые записи.
// Логгер передаётся явно.
func NewFileStorage(path string, log *zap.Logger) (*FileStorage, error) {
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
		log:     log,
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
		if rec.IsDeleted {
			_ = mem.MarkDeleted(context.Background(), rec.UserID, []string{rec.ShortURL})
			continue
		}
		_ = mem.Save(context.Background(), rec.ShortURL, rec.OriginalURL, rec.UserID)
		if n, convErr := strconv.Atoi(rec.UUID); convErr == nil && n > nextID {
			nextID = n
		}
	}
	return nextID, nil
}

// Save сохраняет ссылку в память и дописывает её в файл.
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
		s.log.Warn("failed to persist record", zap.Error(err))
		return err
	}
	if err := s.mem.Save(ctx, id, url, userID); err != nil {
		return err
	}
	s.nextID++
	return nil
}

// SaveBatch сохраняет пачку ссылок одной записью в файл. В файл попадают только
// новые адреса: для уже известного адреса возвращается выданная ранее ссылка, и
// хранилище не копит записи, которые при загрузке всё равно будут отброшены.
func (s *FileStorage) SaveBatch(ctx context.Context, items []BatchItem, userID string) ([]BatchItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved := make([]BatchItem, 0, len(items))
	fresh := make([]BatchItem, 0, len(items))
	// ссылки, выданные адресам в этой же пачке: в памяти их ещё нет
	assigned := make(map[string]string, len(items))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, it := range items {
		existing, ok := s.mem.Find(it.URL)
		if !ok {
			existing, ok = assigned[it.URL]
		}
		if ok {
			saved = append(saved, BatchItem{ID: existing, URL: it.URL})
			continue
		}

		rec := &Record{
			UUID:        strconv.Itoa(s.nextID + len(fresh) + 1),
			ShortURL:    it.ID,
			OriginalURL: it.URL,
			UserID:      userID,
		}
		if err := enc.Encode(rec); err != nil {
			s.log.Warn("failed to encode batch record", zap.Error(err))
			return nil, err
		}
		assigned[it.URL] = it.ID
		fresh = append(fresh, it)
		saved = append(saved, it)
	}

	if buf.Len() > 0 {
		if _, err := s.file.Write(buf.Bytes()); err != nil {
			s.log.Warn("failed to persist batch", zap.Error(err))
			return nil, err
		}
	}
	if _, err := s.mem.SaveBatch(ctx, fresh, userID); err != nil {
		return nil, err
	}
	// счётчик двигаем последним: пока пачка не принята целиком, нумерация
	// уходит вперёд от того, что хранилище готово отдавать
	s.nextID += len(fresh)
	return saved, nil
}

// Get возвращает оригинальный адрес по короткой ссылке.
func (s *FileStorage) Get(ctx context.Context, id string) (string, error) {
	return s.mem.Get(ctx, id)
}

// ListByUser возвращает ссылки пользователя.
func (s *FileStorage) ListByUser(ctx context.Context, userID string) ([]UserURL, error) {
	return s.mem.ListByUser(ctx, userID)
}

// MarkDeleted помечает ссылки удалёнными и фиксирует это в файле.
func (s *FileStorage) MarkDeleted(ctx context.Context, userID string, ids []string) error {
	if userID == "" || len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, id := range ids {
		if err := enc.Encode(&Record{ShortURL: id, UserID: userID, IsDeleted: true}); err != nil {
			s.log.Warn("failed to encode delete record", zap.Error(err))
			return err
		}
	}
	if _, err := s.file.Write(buf.Bytes()); err != nil {
		s.log.Warn("failed to persist delete", zap.Error(err))
		return err
	}
	return s.mem.MarkDeleted(ctx, userID, ids)
}

// Close закрывает файл хранилища.
func (s *FileStorage) Close() error {
	return s.file.Close()
}
