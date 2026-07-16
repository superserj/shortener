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
func NewHTTPSink(url string) *HTTPSink {
	return &HTTPSink{
		url:    url,
		client: &http.Client{Timeout: httpTimeout},
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
	io.Copy(io.Discard, resp.Body)

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
