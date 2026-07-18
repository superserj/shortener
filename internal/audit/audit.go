// Пакет audit рассылает события об обработанных запросах по приёмникам аудита.
// Реализует паттерн «Наблюдатель»: издателем выступает Auditor, подписчиками —
// приёмники, реализующие интерфейс Sink.
package audit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Action — действие, которое зафиксировало событие аудита.
type Action string

// Действия, попадающие в аудит.
const (
	// ActionShorten — создание сокращённой ссылки.
	ActionShorten Action = "shorten"
	// ActionFollow — переход по сокращённой ссылке.
	ActionFollow Action = "follow"
)

const (
	queueSize           = 256
	sendTimeout         = 3 * time.Second
	defaultDrainTimeout = 5 * time.Second
)

// Event — событие аудита одного обработанного запроса.
type Event struct {
	// TS — время события в формате unix timestamp.
	TS int64 `json:"ts"`
	// Action — что произошло: создание ссылки или переход по ней.
	Action Action `json:"action"`
	// UserID — идентификатор пользователя, если он известен.
	UserID string `json:"user_id,omitempty"`
	// URL — оригинальный, не сокращённый адрес.
	URL string `json:"url"`
}

// NewEvent собирает событие с текущей меткой времени.
func NewEvent(action Action, userID, url string) Event {
	return Event{
		TS:     time.Now().Unix(),
		Action: action,
		UserID: userID,
		URL:    url,
	}
}

// Sink — приёмник событий аудита, подписчик в терминах паттерна «Наблюдатель».
type Sink interface {
	// Send отправляет одно событие в приёмник.
	Send(ctx context.Context, e Event) error
	// Close освобождает ресурсы приёмника.
	Close() error
}

// Auditor рассылает события всем зарегистрированным приёмникам.
// Нулевое значение не готово к работе, используйте New.
type Auditor struct {
	mu           sync.Mutex
	wg           sync.WaitGroup // счётчик незавершённых Notify
	sinks        []Sink
	workers      []*sinkWorker
	closed       bool
	log          *zap.Logger
	drainTimeout time.Duration
}

// New создаёт аудитор без приёмников. Логгер передаётся явно, без глобального
// состояния.
func New(log *zap.Logger) *Auditor {
	return &Auditor{log: log, drainTimeout: defaultDrainTimeout}
}

// Register подписывает приёмник на события. Регистрировать приёмники нужно до
// запуска Run: на каждый приёмник заводится своя очередь и своя горутина, а Run
// лишь запускает эти горутины по снимку списка. Пока не зарегистрирован ни один
// приёмник, Notify не делает ничего.
func (a *Auditor) Register(s Sink) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sinks = append(a.sinks, s)
	a.workers = append(a.workers, newSinkWorker(s, a.log))
}

// Notify публикует событие и сразу возвращает управление. Событие кладётся в
// очередь каждого приёмника независимо и без блокировки: если очередь приёмника
// переполнена, для него событие отбрасывается, а остальные его получают. Так
// медленный приёмник забивает только свою очередь и не задерживает ни ответ
// сервиса, ни доставку другим приёмникам. Счётчик wg не даёт shutdown закрыть
// очереди, пока не отработают уже принятые Notify.
func (a *Auditor) Notify(e Event) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.wg.Add(1)
	workers := a.workers
	a.mu.Unlock()
	defer a.wg.Done()

	for _, w := range workers {
		select {
		case w.ch <- e:
		default:
			a.log.Warn("audit queue is full, event dropped", zap.String("action", string(e.Action)))
		}
	}
}

// Run запускает по горутине на приёмник (число горутин равно числу приёмников и
// не растёт) и работает до отмены контекста. Каждая горутина обслуживает свою
// очередь независимо, поэтому медленный приёмник не задерживает остальных. По
// отмене best-effort дорассылает уже принятые события и закрывает приёмники, но
// не дольше drainTimeout: доставка ограничена этим бюджетом, чтобы зависший или
// сильно отстающий приёмник не задерживал остановку (остаток очереди дропается).
func (a *Auditor) Run(ctx context.Context) {
	a.mu.Lock()
	workers := a.workers
	a.mu.Unlock()

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(w *sinkWorker) {
			defer wg.Done()
			w.run()
		}(w)
	}

	<-ctx.Done()
	a.shutdown(workers, &wg)
}

// shutdown закрывает очереди приёмников и дожидается их горутин, но не дольше
// drainTimeout. Каждая горутина сама дошлёт остаток своей очереди и закроет свой
// приёмник (см. sinkWorker.run), поэтому здесь нет общего закрытия приёмников и,
// как следствие, нет гонки Send/Close: приёмник трогает только его собственная
// горутина. Незавершившиеся к дедлайну горутины бросаем — процесс всё равно
// останавливается.
func (a *Auditor) shutdown(workers []*sinkWorker, wg *sync.WaitGroup) {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()

	// Ждём завершения принятых Notify, иначе закрытие очереди могло бы совпасть
	// с записью в неё (паника «send on closed channel»).
	a.wg.Wait()
	for _, w := range workers {
		close(w.ch)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(a.drainTimeout):
		a.log.Warn("audit drain timed out, sink workers still busy")
	}
}

// sinkWorker обслуживает один приёмник: последовательно вычитывает свою очередь,
// отправляет события и по опустошении очереди закрывает приёмник. Отдельная
// очередь и единственная горутина на приёмник изолируют его от остальных и
// исключают конкурентный доступ к приёмнику.
type sinkWorker struct {
	sink Sink
	ch   chan Event
	log  *zap.Logger
}

func newSinkWorker(s Sink, log *zap.Logger) *sinkWorker {
	return &sinkWorker{sink: s, ch: make(chan Event, queueSize), log: log}
}

// run вычитывает очередь, пока она не будет закрыта и опустошена, затем закрывает
// приёмник. Контекст для Send всегда свежий (background с таймаутом): отмена Run
// не должна доходить до приёмника уже отменённой.
func (w *sinkWorker) run() {
	defer func() {
		if err := w.sink.Close(); err != nil {
			w.log.Warn("audit sink close failed", zap.Error(err))
		}
	}()
	for e := range w.ch {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		if err := w.sink.Send(ctx, e); err != nil {
			w.log.Warn("audit send failed", zap.Error(err))
		}
		cancel()
	}
}
