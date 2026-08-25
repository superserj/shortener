package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemStorageSaveBatchKeepsExistingShortURL(t *testing.T) {
	ctx := context.Background()
	s := NewMemStorage()
	require.NoError(t, s.Save(ctx, "old1", "https://example.com/1", "user1"))

	saved, err := s.SaveBatch(ctx, []BatchItem{
		{ID: "new1", URL: "https://example.com/1"},
		{ID: "new2", URL: "https://example.com/2"},
	}, "user2")
	require.NoError(t, err)
	require.Len(t, saved, 2)

	assert.Equal(t, "old1", saved[0].ID, "известный адрес сохраняет выданную ранее ссылку")
	assert.Equal(t, "new2", saved[1].ID)

	_, err = s.Get(ctx, "new1")
	assert.ErrorIs(t, err, ErrNotFound, "лишняя ссылка на известный адрес не появляется")

	got, err := s.Get(ctx, "new2")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/2", got)
}

func TestMemStorageSaveBatchDeduplicatesInsideBatch(t *testing.T) {
	ctx := context.Background()
	s := NewMemStorage()

	saved, err := s.SaveBatch(ctx, []BatchItem{
		{ID: "first", URL: "https://example.com/dup"},
		{ID: "second", URL: "https://example.com/dup"},
	}, "user1")
	require.NoError(t, err)
	require.Len(t, saved, 2)

	assert.Equal(t, "first", saved[0].ID)
	assert.Equal(t, "first", saved[1].ID, "повтор в пачке получает ссылку первого вхождения")

	_, err = s.Get(ctx, "second")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemStorageSaveBatchKeepsOwner(t *testing.T) {
	ctx := context.Background()
	s := NewMemStorage()
	require.NoError(t, s.Save(ctx, "old1", "https://example.com/1", "user1"))

	_, err := s.SaveBatch(ctx, []BatchItem{{ID: "new1", URL: "https://example.com/1"}}, "user2")
	require.NoError(t, err)

	urls, err := s.ListByUser(ctx, "user1")
	require.NoError(t, err)
	require.Len(t, urls, 1, "владелец адреса не меняется")

	urls, err = s.ListByUser(ctx, "user2")
	require.NoError(t, err)
	assert.Empty(t, urls)
}

func TestMemStorageStats(t *testing.T) {
	ctx := context.Background()
	s := NewMemStorage()

	stats, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, Stats{}, stats, "пустое хранилище отдаёт нули")

	require.NoError(t, s.Save(ctx, "id1", "https://example.com/1", "user1"))
	require.NoError(t, s.Save(ctx, "id2", "https://example.com/2", "user1"))
	require.NoError(t, s.Save(ctx, "id3", "https://example.com/3", "user2"))
	require.NoError(t, s.Save(ctx, "id4", "https://example.com/4", ""))

	stats, err = s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, stats.URLs)
	assert.Equal(t, 2, stats.Users, "ссылка без пользователя счётчик не увеличивает")

	require.NoError(t, s.MarkDeleted(ctx, "user2", []string{"id3"}))

	stats, err = s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, stats.URLs, "удаление не переписывает историю сервиса")
	assert.Equal(t, 2, stats.Users, "и не отменяет пользователя, убравшего свою ссылку")
}
