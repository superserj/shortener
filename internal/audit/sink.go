package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	fileMode        = 0666
	httpTimeout     = 3 * time.Second
	contentTypeJSON = "application/json"
	// httpMaxAttempts — общее число попыток отправки: одна основная плюс ретраи.
	httpMaxAttempts = 3
	// httpRetryBackoff — базовая пауза между попытками; растёт линейно с номером попытки.
	httpRetryBackoff = 100 * time.Millisecond
)

// FileSink дописывает события в конец файла, по одному JSON в строке.
type FileSink struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

// NewFileSink открывает файл-приёмник на дозапись, создавая его при необходимости.
func NewFileSink(path string) (*FileSink, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return nil, err
	}
	return &FileSink{file: file, encoder: json.NewEncoder(file)}, nil
}

// Send дописывает событие в конец файла отдельной строкой.
func (s *FileSink) Send(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.encoder.Encode(e)
}

// Close закрывает файл-приёмник.
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

// HTTPSink отправляет события на удалённый сервер-приёмник методом POST.
type HTTPSink struct {
	url    string
	client *http.Client
}

// NewHTTPSink создаёт приёмник, отправляющий события по указанному URL.
// Клиент прозрачно повторяет запрос на сетевых ошибках и 5xx: код Send при этом
// не меняется, ретраи скрыты в транспорте.
func NewHTTPSink(url string) *HTTPSink {
	return &HTTPSink{
		url: url,
		client: &http.Client{
			Timeout:   httpTimeout,
			Transport: &retryTransport{base: http.DefaultTransport, attempts: httpMaxAttempts, backoff: httpRetryBackoff},
		},
	}
}

// Send отправляет событие на удалённый сервер. Ответ с кодом 4xx или 5xx
// считается ошибкой отправки.
func (s *HTTPSink) Send(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("audit sink returned %s", resp.Status)
	}
	return nil
}

// Close закрывает неиспользуемые соединения с сервером-приёмником.
func (s *HTTPSink) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

// retryTransport — http.RoundTripper, повторяющий запрос на сетевых ошибках и 5xx.
// Доставка событий аудита имеет семантику at-least-once: повтор POST может
// продублировать запись в приёмнике, что для журнала аудита допустимо.
type retryTransport struct {
	base     http.RoundTripper
	attempts int
	backoff  time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)
	for attempt := 1; attempt <= t.attempts; attempt++ {
		// Тело запроса читается на каждой попытке заново: bytes.Reader выставляет
		// GetBody, поэтому повтор POST с телом безопасен.
		if attempt > 1 && req.GetBody != nil {
			body, gerr := req.GetBody()
			if gerr != nil {
				return nil, gerr
			}
			req.Body = body
		}
		resp, err = t.base.RoundTrip(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		}
		if attempt == t.attempts {
			break
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(t.backoff * time.Duration(attempt)):
		}
	}
	return resp, err
}
