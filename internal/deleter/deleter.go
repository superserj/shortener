// Пакет deleter удаляет ссылки пользователей в фоне, накапливая их в батчи.
package deleter

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/storage"
)

const (
	maxWriters  = 16
	flushPeriod = 2 * time.Second
)

type item struct {
	userID string
	id     string
}

// Worker собирает ссылки на удаление и применяет их пачками.
type Worker struct {
	in     chan item
	sem    chan struct{}
	store  storage.Repository
	period time.Duration
	log    *zap.Logger

	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

// New создаёт воркер удаления поверх указанного хранилища. Логгер передаётся явно.
func New(store storage.Repository, log *zap.Logger) *Worker {
	return &Worker{
		in:     make(chan item, 1),
		sem:    make(chan struct{}, maxWriters),
		store:  store,
		period: flushPeriod,
		log:    log,
	}
}

// Enqueue ставит ссылки пользователя в очередь на удаление, не дожидаясь его.
func (w *Worker) Enqueue(userID string, ids []string) {
	if userID == "" || len(ids) == 0 {
		return
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.wg.Add(1)
	w.mu.Unlock()

	w.sem <- struct{}{}
	go func() {
		defer w.wg.Done()
		defer func() { <-w.sem }()
		for _, id := range ids {
			w.in <- item{userID: userID, id: id}
		}
	}()
}

// Run обрабатывает очередь до отмены контекста, после чего дочищает остаток.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.period)
	defer ticker.Stop()

	buf := make(map[string][]string)
	for {
		select {
		case it := <-w.in:
			buf[it.userID] = append(buf[it.userID], it.id)
		case <-ticker.C:
			w.flush(ctx, buf)
		case <-ctx.Done():
			w.shutdown(buf)
			return
		}
	}
}

func (w *Worker) shutdown(buf map[string][]string) {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()

	go func() {
		w.wg.Wait()
		close(w.in)
	}()
	for it := range w.in {
		buf[it.userID] = append(buf[it.userID], it.id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	w.flush(ctx, buf)
	cancel()
}

func (w *Worker) flush(ctx context.Context, buf map[string][]string) {
	for uid, ids := range buf {
		if err := w.store.MarkDeleted(ctx, uid, ids); err != nil {
			w.log.Warn("mark deleted failed", zap.String("user", uid), zap.Error(err))
		}
		delete(buf, uid)
	}
}
