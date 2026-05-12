package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStoragePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")
	ctx := context.Background()

	first, err := NewFileStorage(path)
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, "abc", "https://practicum.yandex.ru/"))
	require.NoError(t, first.Save(ctx, "def", "https://example.com/"))
	require.NoError(t, first.Close())

	second, err := NewFileStorage(path)
	require.NoError(t, err)
	defer second.Close()

	got, err := second.Get(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, "https://practicum.yandex.ru/", got)

	got, err = second.Get(ctx, "def")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/", got)
}

func TestFileStorageEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")

	s, err := NewFileStorage(path)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.Get(context.Background(), "anything")
	assert.True(t, errors.Is(err, ErrNotFound))
}
