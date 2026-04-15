package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superserj/shortener/internal/storage"
)

func setupRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Get("/{id}", h.Redirect)
	return r
}

func TestShortenURL(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080")

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid url",
			body:       "https://practicum.yandex.ru/",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "empty body",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			h.ShortenURL(w, r)

			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			if tt.wantStatus == http.StatusCreated {
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), "http://localhost:8080/")
				assert.Equal(t, "text/plain", res.Header.Get("Content-Type"))
			}
		})
	}
}

func TestShortenAPI(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080")

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid url",
			body:       `{"url":"https://practicum.yandex.ru/"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "empty url",
			body:       `{"url":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `not a json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			h.ShortenAPI(w, r)

			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			if tt.wantStatus == http.StatusCreated {
				assert.Equal(t, "application/json", res.Header.Get("Content-Type"))
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), `"result":"http://localhost:8080/`)
			}
		})
	}
}

func TestRedirect(t *testing.T) {
	store := storage.NewMemStorage()
	store.Save("testid", "https://practicum.yandex.ru/")
	h := New(store, "http://localhost:8080")

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantURL    string
	}{
		{
			name:       "existing id",
			path:       "/testid",
			wantStatus: http.StatusTemporaryRedirect,
			wantURL:    "https://practicum.yandex.ru/",
		},
		{
			name:       "missing id",
			path:       "/unknown",
			wantStatus: http.StatusBadRequest,
		},
	}

	ts := httptest.NewServer(setupRouter(h))
	defer ts.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			resp, err := client.Get(ts.URL + tt.path)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantStatus == http.StatusTemporaryRedirect {
				assert.Equal(t, tt.wantURL, resp.Header.Get("Location"))
			}
		})
	}
}
