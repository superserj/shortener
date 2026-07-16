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

type FileSink struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

func NewFileSink(path string) (*FileSink, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return nil, err
	}
	return &FileSink{file: file, encoder: json.NewEncoder(file)}, nil
}

func (s *FileSink) Send(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.encoder.Encode(e)
}

func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

type HTTPSink struct {
	url    string
	client *http.Client
}

func NewHTTPSink(url string) *HTTPSink {
	return &HTTPSink{
		url:    url,
		client: &http.Client{Timeout: httpTimeout},
	}
}

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

func (s *HTTPSink) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
