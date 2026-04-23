package storage

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStoragePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.json")

	first, err := NewFileStorage(path)
	require.NoError(t, err)
	first.Save("abc", "https://practicum.yandex.ru/")
	first.Save("def", "https://example.com/")
	require.NoError(t, first.Close())

	second, err := NewFileStorage(path)
	require.NoError(t, err)
	defer second.Close()

	got, ok := second.Get("abc")
	assert.True(t, ok)
	assert.Equal(t, "https://practicum.yandex.ru/", got)

	got, ok = second.Get("def")
	assert.True(t, ok)
	assert.Equal(t, "https://example.com/", got)
}

func TestFileStorageEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")

	s, err := NewFileStorage(path)
	require.NoError(t, err)
	defer s.Close()

	_, ok := s.Get("anything")
	assert.False(t, ok)
}
