package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFileStoragePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")
	ctx := context.Background()

	first, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, "abc", "https://practicum.yandex.ru/", "user1"))
	require.NoError(t, first.Save(ctx, "def", "https://example.com/", "user1"))
	require.NoError(t, first.Close())

	second, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	defer second.Close()

	got, err := second.Get(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, "https://practicum.yandex.ru/", got)

	got, err = second.Get(ctx, "def")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/", got)
}

func TestFileStorageDeletePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")
	ctx := context.Background()

	first, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, "abc", "https://practicum.yandex.ru/", "user1"))
	require.NoError(t, first.MarkDeleted(ctx, "user1", []string{"abc"}))
	require.NoError(t, first.Close())

	second, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	defer second.Close()

	_, err = second.Get(ctx, "abc")
	assert.True(t, errors.Is(err, ErrDeleted))
}

func TestFileStorageSaveBatchPersistsOnlyNewURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")
	ctx := context.Background()

	first, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, "old1", "https://example.com/1", "user1"))

	saved, err := first.SaveBatch(ctx, []BatchItem{
		{ID: "new1", URL: "https://example.com/1"},
		{ID: "new2", URL: "https://example.com/2"},
		{ID: "new3", URL: "https://example.com/2"},
	}, "user1")
	require.NoError(t, err)
	require.Len(t, saved, 3)
	assert.Equal(t, "old1", saved[0].ID)
	assert.Equal(t, "new2", saved[1].ID)
	assert.Equal(t, "new2", saved[2].ID)
	require.NoError(t, first.Close())

	second, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	defer second.Close()

	// после перезапуска резолвятся ровно те ссылки, что были в ответе
	for _, it := range saved {
		got, getErr := second.Get(ctx, it.ID)
		require.NoError(t, getErr, "ссылка %s должна пережить перезапуск", it.ID)
		assert.Equal(t, it.URL, got)
	}

	_, err = second.Get(ctx, "new1")
	assert.True(t, errors.Is(err, ErrNotFound), "запись про уже известный адрес в файл не пишется")
}

func TestFileStorageNumbersRecordsConsecutively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")
	ctx := context.Background()

	s, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, s.Save(ctx, "id1", "https://example.com/1", "user1"))

	_, err = s.SaveBatch(ctx, []BatchItem{
		{ID: "id2", URL: "https://example.com/2"},
		{ID: "id3", URL: "https://example.com/1"}, // адрес уже известен, в файл не попадёт
		{ID: "id4", URL: "https://example.com/3"},
	}, "user1")
	require.NoError(t, err)
	require.NoError(t, s.Save(ctx, "id5", "https://example.com/4", "user1"))
	require.NoError(t, s.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var uuids []string
	dec := json.NewDecoder(f)
	for {
		var rec Record
		if decErr := dec.Decode(&rec); decErr != nil {
			require.True(t, errors.Is(decErr, io.EOF))
			break
		}
		uuids = append(uuids, rec.UUID)
	}
	assert.Equal(t, []string{"1", "2", "3", "4"}, uuids, "номера записей идут подряд и без пропусков")
}

func TestFileStorageEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")

	s, err := NewFileStorage(path, zap.NewNop())
	require.NoError(t, err)
	defer s.Close()

	_, err = s.Get(context.Background(), "anything")
	assert.True(t, errors.Is(err, ErrNotFound))
}
