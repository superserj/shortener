// Пакет service содержит операции над ссылками, общие для всех транспортов:
// HTTP- и gRPC-обработчики остаются тонкими фасадами над ним.
package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/superserj/shortener/internal/audit"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

const (
	charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// idLength — длина короткой ссылки. Восьми символов алфавита хватает,
	// чтобы случайные ссылки не сталкивались на объёмах учебного сервиса.
	idLength = 8
)

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// AuditNotifier принимает события аудита обработанных запросов.
type AuditNotifier interface {
	Notify(e audit.Event)
}

// Service сокращает адреса и отдаёт их обратно.
type Service struct {
	store   storage.Repository
	baseURL string
	auditor AuditNotifier
}

// New создаёт сервис поверх хранилища. Аргумент auditor может быть nil: тогда
// аудит не ведётся.
func New(store storage.Repository, baseURL string, auditor AuditNotifier) *Service {
	return &Service{store: store, baseURL: baseURL, auditor: auditor}
}

// Shorten сохраняет адрес и возвращает короткую ссылку целиком. Второе значение
// сообщает, что адрес уже сокращали: ссылка тогда выдана не сейчас, а раньше.
func (s *Service) Shorten(ctx context.Context, originalURL, userID string) (string, bool, error) {
	id := generateID()
	conflict := false

	if err := s.store.Save(ctx, id, originalURL, userID); err != nil {
		var existing *storage.ConflictError
		if !errors.As(err, &existing) {
			return "", false, err
		}
		id = existing.ShortURL
		conflict = true
	}

	s.notify(audit.ActionShorten, userID, originalURL)
	return s.shortURL(id), conflict, nil
}

// ShortenBatch сокращает пачку адресов за одну операцию хранилища. Результат
// повторяет порядок urls: адресу, который уже сокращали, соответствует выданная
// ранее ссылка.
func (s *Service) ShortenBatch(ctx context.Context, urls []string, userID string) ([]string, error) {
	items := make([]storage.BatchItem, 0, len(urls))
	for _, url := range urls {
		items = append(items, storage.BatchItem{ID: generateID(), URL: url})
	}

	saved, err := s.store.SaveBatch(ctx, items, userID)
	if err != nil {
		return nil, err
	}
	if len(saved) != len(items) {
		return nil, fmt.Errorf("batch of %d urls came back with %d links", len(items), len(saved))
	}

	result := make([]string, 0, len(saved))
	for _, it := range saved {
		result = append(result, s.shortURL(it.ID))
	}
	return result, nil
}

// Expand возвращает оригинальный адрес по короткой ссылке. Ошибки хранилища
// (storage.ErrNotFound, storage.ErrDeleted) передаются вызывающему коду как есть.
// Идентификатор пользователя нужен только для аудита и может быть пустым.
func (s *Service) Expand(ctx context.Context, id, userID string) (string, error) {
	originalURL, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}

	s.notify(audit.ActionFollow, userID, originalURL)
	return originalURL, nil
}

// ListUserURLs возвращает ссылки пользователя с полными короткими адресами.
func (s *Service) ListUserURLs(ctx context.Context, userID string) ([]models.UserURLItem, error) {
	urls, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	items := make([]models.UserURLItem, 0, len(urls))
	for _, u := range urls {
		items = append(items, models.UserURLItem{
			ShortURL:    s.shortURL(u.ShortURL),
			OriginalURL: u.OriginalURL,
		})
	}
	return items, nil
}

// Stats возвращает количество ссылок и пользователей сервиса.
func (s *Service) Stats(ctx context.Context) (storage.Stats, error) {
	return s.store.Stats(ctx)
}

// shortURL собирает адрес, по которому сервис отдаёт ссылку клиенту.
func (s *Service) shortURL(id string) string {
	return s.baseURL + "/" + id
}

func (s *Service) notify(action audit.Action, userID, originalURL string) {
	if s.auditor == nil {
		return
	}
	s.auditor.Notify(audit.NewEvent(action, userID, originalURL))
}

func generateID() string {
	rngMu.Lock()
	defer rngMu.Unlock()
	b := make([]byte, idLength)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}
