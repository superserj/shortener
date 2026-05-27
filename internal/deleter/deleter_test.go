package deleter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/storage"
)

func init() {
	_ = logger.Initialize("error")
}

type spyStore struct {
	*storage.MemStorage
	mu    sync.Mutex
	calls []call
}

type call struct {
	userID string
	ids    []string
}

func newSpyStore() *spyStore {
	return &spyStore{MemStorage: storage.NewMemStorage()}
}

func (s *spyStore) MarkDeleted(ctx context.Context, userID string, ids []string) error {
	s.mu.Lock()
	s.calls = append(s.calls, call{userID: userID, ids: append([]string(nil), ids...)})
	s.mu.Unlock()
	return s.MemStorage.MarkDeleted(ctx, userID, ids)
}

func TestWorkerBatchesByUser(t *testing.T) {
	store := newSpyStore()
	require.NoError(t, store.Save(context.Background(), "abc", "https://a.example/", "user-1"))
	require.NoError(t, store.Save(context.Background(), "def", "https://b.example/", "user-1"))
	require.NoError(t, store.Save(context.Background(), "ghi", "https://c.example/", "user-2"))

	w := New(store)
	w.period = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	w.Enqueue(ctx, "user-1", []string{"abc", "def"})
	w.Enqueue(ctx, "user-2", []string{"ghi"})

	assert.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.calls) >= 2
	}, time.Second, 20*time.Millisecond)

	cancel()
	<-done

	_, err := store.Get(context.Background(), "abc")
	assert.ErrorIs(t, err, storage.ErrDeleted)
	_, err = store.Get(context.Background(), "def")
	assert.ErrorIs(t, err, storage.ErrDeleted)
	_, err = store.Get(context.Background(), "ghi")
	assert.ErrorIs(t, err, storage.ErrDeleted)
}

func TestWorkerFlushesOnShutdown(t *testing.T) {
	store := newSpyStore()
	require.NoError(t, store.Save(context.Background(), "id1", "https://a.example/", "u"))

	w := New(store)
	w.period = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	w.Enqueue(ctx, "u", []string{"id1"})
	time.Sleep(20 * time.Millisecond)

	cancel()
	<-done

	_, err := store.Get(context.Background(), "id1")
	assert.ErrorIs(t, err, storage.ErrDeleted)
}

func TestEnqueueIgnoresEmpty(t *testing.T) {
	w := New(newSpyStore())
	w.Enqueue(context.Background(), "", []string{"x"})
	w.Enqueue(context.Background(), "u", nil)
	assert.Equal(t, 0, len(w.in))
}
