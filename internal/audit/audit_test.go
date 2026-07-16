package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	waitTimeout  = 2 * time.Second
	pollInterval = 5 * time.Millisecond
)

type stubSink struct {
	mu     sync.Mutex
	events []Event
	closed bool
	err    error
}

func newStubSink() *stubSink {
	return &stubSink{}
}

func (s *stubSink) Send(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return s.err
}

func (s *stubSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *stubSink) snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *stubSink) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func waitEvents(t *testing.T, s *stubSink, n int) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if len(s.snapshot()) >= n {
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("timed out waiting for %d events, got %d", n, len(s.snapshot()))
}

func runAuditor(t *testing.T, a *Auditor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Error("auditor did not stop in time")
		}
	})
}

func TestNewEvent(t *testing.T) {
	before := time.Now().Unix()
	e := NewEvent(ActionShorten, "user-1", "https://example.com/long")
	after := time.Now().Unix()

	assert.Equal(t, ActionShorten, e.Action)
	assert.Equal(t, "user-1", e.UserID)
	assert.Equal(t, "https://example.com/long", e.URL)
	assert.GreaterOrEqual(t, e.TS, before)
	assert.LessOrEqual(t, e.TS, after)
}

func TestEventJSONFormat(t *testing.T) {
	b, err := json.Marshal(Event{TS: 12345678, Action: ActionShorten, UserID: "12315134", URL: "https://mylongdomain.com/path"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"ts":12345678,"action":"shorten","user_id":"12315134","url":"https://mylongdomain.com/path"}`, string(b))
}

func TestEventWithoutUserIDOmitsField(t *testing.T) {
	b, err := json.Marshal(Event{TS: 1, Action: ActionFollow, URL: "https://example.com"})
	require.NoError(t, err)

	assert.NotContains(t, string(b), "user_id")
}

func TestAuditorNotifiesAllSinks(t *testing.T) {
	first, second := newStubSink(), newStubSink()
	a := New()
	a.Register(first)
	a.Register(second)
	runAuditor(t, a)

	a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/one"))
	waitEvents(t, first, 1)
	waitEvents(t, second, 1)

	for _, s := range []*stubSink{first, second} {
		events := s.snapshot()
		require.Len(t, events, 1)
		assert.Equal(t, ActionShorten, events[0].Action)
		assert.Equal(t, "https://example.com/one", events[0].URL)
	}
}

func TestAuditorKeepsGoingWhenSinkFails(t *testing.T) {
	failing, healthy := newStubSink(), newStubSink()
	failing.err = assert.AnError
	a := New()
	a.Register(failing)
	a.Register(healthy)
	runAuditor(t, a)

	a.Notify(NewEvent(ActionFollow, "", "https://example.com/two"))
	waitEvents(t, healthy, 1)

	assert.Len(t, healthy.snapshot(), 1)
}

func TestAuditorWithoutSinksDropsEvents(t *testing.T) {
	a := New()
	runAuditor(t, a)

	a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/three"))

	assert.Empty(t, a.in, "событие не должно попадать в очередь без приёмников")
}

// blockingSink имитирует приёмник, который не отвечает.
type blockingSink struct {
	release chan struct{}
}

func (s *blockingSink) Send(_ context.Context, _ Event) error {
	<-s.release
	return nil
}

func (s *blockingSink) Close() error { return nil }

func TestNotifyDoesNotBlockWhenSinkHangs(t *testing.T) {
	// событий заведомо больше, чем вмещает очередь
	const total = queueSize * 2

	sink := &blockingSink{release: make(chan struct{})}
	a := New()
	a.Register(sink)
	runAuditor(t, a)
	defer close(sink.release)

	done := make(chan struct{})
	go func() {
		for i := 0; i < total; i++ {
			a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/load"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Notify заблокировался на неотвечающем приёмнике")
	}
}

func TestAuditorDeliversBurstWithinQueue(t *testing.T) {
	const total = queueSize

	sink := newStubSink()
	a := New()
	a.Register(sink)
	runAuditor(t, a)

	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/load"))
		}()
	}
	wg.Wait()
	waitEvents(t, sink, total)

	assert.Len(t, sink.snapshot(), total, "всплеск в пределах очереди не должен терять события")
}

func TestAuditorDeliversEventsAcceptedBeforeShutdown(t *testing.T) {
	const total = 64

	sink := newStubSink()
	a := New()
	a.Register(sink)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Notify(NewEvent(ActionFollow, "u1", "https://example.com/racy"))
		}()
	}
	wg.Wait()
	cancel()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("auditor did not stop in time")
	}

	assert.Len(t, sink.snapshot(), total, "принятые до остановки события должны дойти до приёмника")
}

func TestAuditorDrainsAndClosesSinksOnShutdown(t *testing.T) {
	sink := newStubSink()
	a := New()
	a.Register(sink)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/four"))
	waitEvents(t, sink, 1)

	cancel()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("auditor did not stop in time")
	}

	assert.True(t, sink.isClosed(), "приёмник должен закрываться при остановке")

	a.Notify(NewEvent(ActionFollow, "u1", "https://example.com/five"))
	assert.Len(t, sink.snapshot(), 1, "после остановки события не принимаются")
}

// приёмник, который запоминает состояние контекста на момент отправки
type ctxSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *ctxSink) Send(ctx context.Context, _ Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, ctx.Err())
	return nil
}

func (s *ctxSink) Close() error { return nil }

func (s *ctxSink) liveCount() (live, dead int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, err := range s.errs {
		if err == nil {
			live++
		} else {
			dead++
		}
	}
	return live, dead
}

func TestSinksNeverGetCancelledContext(t *testing.T) {
	const (
		trials = 50
		events = 8
	)

	for i := 0; i < trials; i++ {
		sink := &ctxSink{}
		a := New()
		a.Register(sink)

		ctx, cancel := context.WithCancel(context.Background())
		for j := 0; j < events; j++ {
			a.Notify(NewEvent(ActionShorten, "u1", "https://example.com/queued"))
		}
		// отменяем при непустой очереди: обе ветки select готовы
		cancel()

		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Fatal("auditor did not stop in time")
		}

		live, dead := sink.liveCount()
		require.Zero(t, dead, "приёмник не должен получать отменённый контекст")
		require.Equal(t, events, live, "все принятые события должны дойти до приёмника")
	}
}

func TestFileSinkAppendsOnePerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	sink, err := NewFileSink(path)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), Event{TS: 1, Action: ActionShorten, UserID: "u1", URL: "https://example.com/one"}))
	require.NoError(t, sink.Send(context.Background(), Event{TS: 2, Action: ActionFollow, URL: "https://example.com/two"}))
	require.NoError(t, sink.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 2)

	var first Event
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, Event{TS: 1, Action: ActionShorten, UserID: "u1", URL: "https://example.com/one"}, first)
}

func TestFileSinkAppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	require.NoError(t, os.WriteFile(path, []byte("{\"ts\":1,\"action\":\"shorten\",\"url\":\"https://example.com/old\"}\n"), fileMode))

	sink, err := NewFileSink(path)
	require.NoError(t, err)
	require.NoError(t, sink.Send(context.Background(), Event{TS: 2, Action: ActionFollow, URL: "https://example.com/new"}))
	require.NoError(t, sink.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 2, "существующие записи не должны затираться")
	assert.Contains(t, lines[0], "https://example.com/old")
	assert.Contains(t, lines[1], "https://example.com/new")
}

func TestFileSinkFailsOnBadPath(t *testing.T) {
	_, err := NewFileSink(filepath.Join(t.TempDir(), "missing", "audit.log"))
	assert.Error(t, err)
}

func TestHTTPSinkPostsEvent(t *testing.T) {
	type request struct {
		method      string
		contentType string
		body        Event
	}
	got := make(chan request, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e Event
		require.NoError(t, json.NewDecoder(r.Body).Decode(&e))
		got <- request{method: r.Method, contentType: r.Header.Get("Content-Type"), body: e}
	}))
	defer srv.Close()

	sink := NewHTTPSink(srv.URL)
	defer sink.Close()

	want := Event{TS: 42, Action: ActionFollow, UserID: "u1", URL: "https://example.com/long"}
	require.NoError(t, sink.Send(context.Background(), want))

	select {
	case r := <-got:
		assert.Equal(t, http.MethodPost, r.method)
		assert.Equal(t, contentTypeJSON, r.contentType)
		assert.Equal(t, want, r.body)
	case <-time.After(waitTimeout):
		t.Fatal("remote sink got no request")
	}
}

func TestHTTPSinkReportsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink := NewHTTPSink(srv.URL)
	defer sink.Close()

	assert.Error(t, sink.Send(context.Background(), Event{TS: 1, Action: ActionShorten, URL: "https://example.com"}))
}

func TestHTTPSinkReportsUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	sink := NewHTTPSink(url)
	defer sink.Close()

	assert.Error(t, sink.Send(context.Background(), Event{TS: 1, Action: ActionShorten, URL: "https://example.com"}))
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	require.NoError(t, sc.Err())
	return lines
}
