package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/superserj/shortener/internal/audit"
	"github.com/superserj/shortener/internal/storage"
)

const testBaseURL = "http://localhost:8080"

// recordAuditor запоминает события, чтобы проверить, что сервис их шлёт.
type recordAuditor struct {
	events []audit.Event
}

func (r *recordAuditor) Notify(e audit.Event) {
	r.events = append(r.events, e)
}

// id вырезает короткую ссылку из полного адреса.
func id(t *testing.T, shortURL string) string {
	t.Helper()
	require.True(t, strings.HasPrefix(shortURL, testBaseURL+"/"))
	return strings.TrimPrefix(shortURL, testBaseURL+"/")
}

func TestShorten(t *testing.T) {
	ctx := context.Background()
	rec := &recordAuditor{}
	svc := New(storage.NewMemStorage(), testBaseURL, rec)

	shortURL, conflict, err := svc.Shorten(ctx, "https://practicum.yandex.ru/", "user1")
	require.NoError(t, err)
	assert.False(t, conflict)
	assert.Len(t, id(t, shortURL), idLength)

	again, conflict, err := svc.Shorten(ctx, "https://practicum.yandex.ru/", "user1")
	require.NoError(t, err)
	assert.True(t, conflict, "известный адрес отмечается как уже сокращённый")
	assert.Equal(t, shortURL, again, "и получает выданную ранее ссылку")

	require.Len(t, rec.events, 2)
	assert.Equal(t, audit.ActionShorten, rec.events[0].Action)
	assert.Equal(t, "user1", rec.events[0].UserID)
}

func TestShortenBatch(t *testing.T) {
	ctx := context.Background()
	svc := New(storage.NewMemStorage(), testBaseURL, nil)

	first, _, err := svc.Shorten(ctx, "https://example.com/1", "user1")
	require.NoError(t, err)

	shortURLs, err := svc.ShortenBatch(ctx, []string{
		"https://example.com/1",
		"https://example.com/2",
	}, "user1")
	require.NoError(t, err)
	require.Len(t, shortURLs, 2)

	assert.Equal(t, first, shortURLs[0], "известный адрес сохраняет прежнюю ссылку")

	original, err := svc.Expand(ctx, id(t, shortURLs[1]), "user1")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/2", original)
}

func TestExpand(t *testing.T) {
	ctx := context.Background()
	rec := &recordAuditor{}
	store := storage.NewMemStorage()
	svc := New(store, testBaseURL, rec)

	require.NoError(t, store.Save(ctx, "id1", "https://example.com/1", "user1"))

	original, err := svc.Expand(ctx, "id1", "user1")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/1", original)
	require.Len(t, rec.events, 1)
	assert.Equal(t, audit.ActionFollow, rec.events[0].Action)

	_, err = svc.Expand(ctx, "unknown1", "user1")
	assert.ErrorIs(t, err, storage.ErrNotFound)
	assert.Len(t, rec.events, 1, "неудачный переход в аудит не попадает")
}

func TestListUserURLsAndStats(t *testing.T) {
	ctx := context.Background()
	svc := New(storage.NewMemStorage(), testBaseURL, nil)

	shortURL, _, err := svc.Shorten(ctx, "https://example.com/1", "user1")
	require.NoError(t, err)
	_, _, err = svc.Shorten(ctx, "https://example.com/2", "user2")
	require.NoError(t, err)

	urls, err := svc.ListUserURLs(ctx, "user1")
	require.NoError(t, err)
	require.Len(t, urls, 1)
	assert.Equal(t, shortURL, urls[0].ShortURL, "ссылка отдаётся полным адресом")
	assert.Equal(t, "https://example.com/1", urls[0].OriginalURL)

	stats, err := svc.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, storage.Stats{URLs: 2, Users: 2}, stats)
}

func TestServiceWithoutAuditor(t *testing.T) {
	ctx := context.Background()
	svc := New(storage.NewMemStorage(), testBaseURL, nil)

	_, _, err := svc.Shorten(ctx, "https://example.com/1", "user1")
	assert.NoError(t, err, "сервис без аудита работает так же")
}
