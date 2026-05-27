package deleter

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/storage"
)

const (
	defaultChannelSize = 256
	defaultFlushPeriod = 2 * time.Second
)

type item struct {
	userID string
	id     string
}

type Worker struct {
	in     chan item
	store  storage.Repository
	period time.Duration
	wg     sync.WaitGroup
}

func New(store storage.Repository) *Worker {
	return &Worker{
		in:     make(chan item, defaultChannelSize),
		store:  store,
		period: defaultFlushPeriod,
	}
}

func (w *Worker) Enqueue(ctx context.Context, userID string, ids []string) {
	if userID == "" || len(ids) == 0 {
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for _, id := range ids {
			select {
			case w.in <- item{userID: userID, id: id}:
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.period)
	defer ticker.Stop()

	buf := make(map[string][]string)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		for uid, ids := range buf {
			if err := w.store.MarkDeleted(ctx, uid, ids); err != nil {
				logger.Log.Warn("mark deleted failed", zap.String("user", uid), zap.Error(err))
			}
		}
		buf = make(map[string][]string)
	}

	for {
		select {
		case it := <-w.in:
			buf[it.userID] = append(buf[it.userID], it.id)
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			w.wg.Wait()
			for {
				select {
				case it := <-w.in:
					buf[it.userID] = append(buf[it.userID], it.id)
				default:
					flush()
					return
				}
			}
		}
	}
}
