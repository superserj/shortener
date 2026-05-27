package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

func init() {
	_ = logger.Initialize("error")
}

type noopDeleter struct{}

func (noopDeleter) Enqueue(_ context.Context, _ string, _ []string) {}

type recordDeleter struct {
	userID string
	ids    []string
}

func (r *recordDeleter) Enqueue(_ context.Context, userID string, ids []string) {
	r.userID = userID
	r.ids = append(r.ids, ids...)
}

func setupRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Get("/{id}", h.Redirect)
	return r
}

func TestShortenURL(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{})

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
	h := New(store, "http://localhost:8080", nil, noopDeleter{})

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

func TestShortenBatch(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{})

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCount  int
	}{
		{
			name:       "valid batch",
			body:       `[{"correlation_id":"a","original_url":"https://example.com/1"},{"correlation_id":"b","original_url":"https://example.com/2"}]`,
			wantStatus: http.StatusCreated,
			wantCount:  2,
		},
		{
			name:       "empty batch",
			body:       `[]`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty url in batch",
			body:       `[{"correlation_id":"a","original_url":""}]`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `not json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			h.ShortenBatch(w, r)

			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			if tt.wantStatus == http.StatusCreated {
				assert.Equal(t, "application/json", res.Header.Get("Content-Type"))
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				assert.Equal(t, tt.wantCount, strings.Count(string(body), `"short_url"`))
				assert.Contains(t, string(body), `"correlation_id":"a"`)
			}
		})
	}
}

func TestUserURLs(t *testing.T) {
	const userID = "test-user"
	ctx := auth.WithUserID(context.Background(), userID)

	store := storage.NewMemStorage()
	require.NoError(t, store.Save(ctx, "ab1", "https://practicum.yandex.ru/", userID))
	require.NoError(t, store.Save(ctx, "cd2", "https://example.com/", userID))
	require.NoError(t, store.Save(ctx, "zz9", "https://other.example.com/", "another-user"))

	h := New(store, "http://localhost:8080", nil, noopDeleter{})

	t.Run("returns urls for current user", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		h.UserURLs(w, r)

		res := w.Result()
		defer res.Body.Close()

		require.Equal(t, http.StatusOK, res.StatusCode)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)

		var items []models.UserURLItem
		require.NoError(t, json.Unmarshal(body, &items))
		assert.Len(t, items, 2)
		for _, item := range items {
			assert.Contains(t, item.ShortURL, "http://localhost:8080/")
		}
	})

	t.Run("returns 204 when user has no urls", func(t *testing.T) {
		emptyCtx := auth.WithUserID(context.Background(), "empty-user")
		r := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil).WithContext(emptyCtx)
		w := httptest.NewRecorder()
		h.UserURLs(w, r)

		assert.Equal(t, http.StatusNoContent, w.Result().StatusCode)
	})

	t.Run("returns 401 on invalid cookie", func(t *testing.T) {
		invalidCtx := auth.WithCookieInvalid(context.Background())
		r := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil).WithContext(invalidCtx)
		w := httptest.NewRecorder()
		h.UserURLs(w, r)

		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
}

func TestDeleteUserURLs(t *testing.T) {
	store := storage.NewMemStorage()

	t.Run("accepts ids and enqueues for user", func(t *testing.T) {
		rec := &recordDeleter{}
		h := New(store, "http://localhost:8080", nil, rec)

		body := strings.NewReader(`["a","b","c"]`)
		r := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body).
			WithContext(auth.WithUserID(context.Background(), "user-1"))
		w := httptest.NewRecorder()
		h.DeleteUserURLs(w, r)

		assert.Equal(t, http.StatusAccepted, w.Result().StatusCode)
		assert.Equal(t, "user-1", rec.userID)
		assert.Equal(t, []string{"a", "b", "c"}, rec.ids)
	})

	t.Run("rejects without user", func(t *testing.T) {
		h := New(store, "http://localhost:8080", nil, noopDeleter{})

		body := strings.NewReader(`["a"]`)
		r := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body)
		w := httptest.NewRecorder()
		h.DeleteUserURLs(w, r)

		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		h := New(store, "http://localhost:8080", nil, noopDeleter{})

		body := strings.NewReader(`not-json`)
		r := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body).
			WithContext(auth.WithUserID(context.Background(), "user-1"))
		w := httptest.NewRecorder()
		h.DeleteUserURLs(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})
}

func TestPingWithoutDB(t *testing.T) {
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{})

	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()

	h.Ping(w, r)

	res := w.Result()
	defer res.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestRedirect(t *testing.T) {
	store := storage.NewMemStorage()
	require.NoError(t, store.Save(context.Background(), "testid", "https://practicum.yandex.ru/", ""))
	h := New(store, "http://localhost:8080", nil, noopDeleter{})

	require.NoError(t, store.Save(context.Background(), "deletedid", "https://gone.example.com/", "owner"))
	require.NoError(t, store.MarkDeleted(context.Background(), "owner", []string{"deletedid"}))

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
		{
			name:       "deleted id",
			path:       "/deletedid",
			wantStatus: http.StatusGone,
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
