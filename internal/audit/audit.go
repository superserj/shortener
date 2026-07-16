package audit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/logger"
)

type Action string

const (
	ActionShorten Action = "shorten"
	ActionFollow  Action = "follow"
)

const (
	queueSize    = 256
	sendTimeout  = 3 * time.Second
	drainTimeout = 5 * time.Second
)

type Event struct {
	TS     int64  `json:"ts"`
	Action Action `json:"action"`
	UserID string `json:"user_id,omitempty"`
	URL    string `json:"url"`
}

func NewEvent(action Action, userID, url string) Event {
	return Event{
		TS:     time.Now().Unix(),
		Action: action,
		UserID: userID,
		URL:    url,
	}
}

type Sink interface {
	Send(ctx context.Context, e Event) error
	Close() error
}

type Auditor struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	sinks  []Sink
	in     chan Event
	closed bool
}

func New() *Auditor {
	return &Auditor{in: make(chan Event, queueSize)}
}

func (a *Auditor) Register(s Sink) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sinks = append(a.sinks, s)
}

// Аудит не должен задерживать ответ, поэтому при переполнении очереди
// событие отбрасывается. Счётчик wg не даёт shutdown закрыть канал,
// пока не отработают уже принятые Notify.
func (a *Auditor) Notify(e Event) {
	a.mu.Lock()
	if a.closed || len(a.sinks) == 0 {
		a.mu.Unlock()
		return
	}
	a.wg.Add(1)
	a.mu.Unlock()
	defer a.wg.Done()

	select {
	case a.in <- e:
	default:
		logger.Log.Warn("audit queue is full, event dropped", zap.String("action", string(e.Action)))
	}
}

func (a *Auditor) Run(ctx context.Context) {
	for {
		select {
		case e := <-a.in:
			a.broadcast(e)
		case <-ctx.Done():
			a.shutdown()
			return
		}
	}
}

// Контекст Run сюда не передаём: при остановке с непустой очередью select мог бы
// выбрать ветку чтения, и отправка сорвалась бы на уже отменённом контексте.
func (a *Auditor) broadcast(e Event) {
	a.mu.Lock()
	sinks := make([]Sink, len(a.sinks))
	copy(sinks, a.sinks)
	a.mu.Unlock()

	for _, s := range sinks {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		if err := s.Send(ctx, e); err != nil {
			logger.Log.Warn("audit send failed", zap.Error(err))
		}
		cancel()
	}
}

// Очередь вычитываем до конца даже после drainTimeout, иначе отправители
// остались бы висеть на записи в канал.
func (a *Auditor) shutdown() {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()

	go func() {
		a.wg.Wait()
		close(a.in)
	}()

	deadline := time.Now().Add(drainTimeout)
	dropped := 0
	for e := range a.in {
		if time.Now().After(deadline) {
			dropped++
			continue
		}
		a.broadcast(e)
	}
	if dropped > 0 {
		logger.Log.Warn("audit drain timed out, events dropped", zap.Int("count", dropped))
	}

	a.closeSinks()
}

func (a *Auditor) closeSinks() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, s := range a.sinks {
		if err := s.Close(); err != nil {
			logger.Log.Warn("audit sink close failed", zap.Error(err))
		}
	}
	a.sinks = nil
}
