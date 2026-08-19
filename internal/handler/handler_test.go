package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/audit"
	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

type noopDeleter struct{}

func (noopDeleter) Enqueue(_ string, _ []string) {}

// failingStore подставляется вместо хранилища, чтобы проверить ответы на его
// ошибки. Все методы отвечают одинаково — errStoreUnavailable.
type failingStore struct{}

var errStoreUnavailable = errors.New("storage unavailable")

func (failingStore) Save(_ context.Context, _, _, _ string) error { return errStoreUnavailable }

func (failingStore) SaveBatch(_ context.Context, _ []storage.BatchItem, _ string) ([]storage.BatchItem, error) {
	return nil, errStoreUnavailable
}

func (failingStore) Get(_ context.Context, _ string) (string, error) { return "", errStoreUnavailable }

func (failingStore) ListByUser(_ context.Context, _ string) ([]storage.UserURL, error) {
	return nil, errStoreUnavailable
}

func (failingStore) MarkDeleted(_ context.Context, _ string, _ []string) error {
	return errStoreUnavailable
}

func (failingStore) Stats(_ context.Context) (storage.Stats, error) {
	return storage.Stats{}, errStoreUnavailable
}

type recordDeleter struct {
	userID string
	ids    []string
}

func (r *recordDeleter) Enqueue(userID string, ids []string) {
	r.userID = userID
	r.ids = append(r.ids, ids...)
}

type recordAuditor struct {
	events []audit.Event
}

func (r *recordAuditor) Notify(e audit.Event) {
	r.events = append(r.events, e)
}

func setupRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Get("/{id}", h.Redirect)
	return r
}

func TestShortenURL(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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

func TestShortenURLConflict(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

	const url = "https://practicum.yandex.ru/"

	first := httptest.NewRecorder()
	h.ShortenURL(first, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url)))
	require.Equal(t, http.StatusCreated, first.Result().StatusCode)

	second := httptest.NewRecorder()
	h.ShortenURL(second, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url)))
	res := second.Result()
	defer res.Body.Close()

	assert.Equal(t, http.StatusConflict, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "http://localhost:8080/")
}

func TestShortenAPI(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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

func TestShortenBatchReturnsExistingShortURL(t *testing.T) {
	store := storage.NewMemStorage()
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())
	ctx := context.Background()

	const known = "https://example.com/known"
	require.NoError(t, store.Save(ctx, "known123", known, ""))

	body := `[{"correlation_id":"a","original_url":"` + known + `"},` +
		`{"correlation_id":"b","original_url":"https://example.com/dup"},` +
		`{"correlation_id":"c","original_url":"https://example.com/dup"}]`

	r := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ShortenBatch(w, r)

	res := w.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusCreated, res.StatusCode)

	var items []models.ShortenBatchResponseItem
	require.NoError(t, json.NewDecoder(res.Body).Decode(&items))
	require.Len(t, items, 3)

	assert.Equal(t, []string{"a", "b", "c"},
		[]string{items[0].CorrelationID, items[1].CorrelationID, items[2].CorrelationID},
		"correlation_id остаётся привязанным к своему адресу")
	assert.Equal(t, "http://localhost:8080/known123", items[0].ShortURL,
		"для известного адреса возвращается выданная ранее ссылка")
	assert.Equal(t, items[1].ShortURL, items[2].ShortURL,
		"повтор адреса внутри пачки получает ту же ссылку")

	for _, it := range items {
		id := strings.TrimPrefix(it.ShortURL, "http://localhost:8080/")
		_, err := store.Get(ctx, id)
		assert.NoError(t, err, "ссылка %s должна быть в хранилище", it.ShortURL)
	}
}

func TestUserURLs(t *testing.T) {
	const userID = "test-user"
	ctx := auth.WithUserID(context.Background(), userID)

	store := storage.NewMemStorage()
	require.NoError(t, store.Save(ctx, "ab1", "https://practicum.yandex.ru/", userID))
	require.NoError(t, store.Save(ctx, "cd2", "https://example.com/", userID))
	require.NoError(t, store.Save(ctx, "zz9", "https://other.example.com/", "another-user"))

	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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
		h := New(store, "http://localhost:8080", nil, rec, nil, zap.NewNop())

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
		h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

		body := strings.NewReader(`["a"]`)
		r := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body)
		w := httptest.NewRecorder()
		h.DeleteUserURLs(w, r)

		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

		body := strings.NewReader(`not-json`)
		r := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body).
			WithContext(auth.WithUserID(context.Background(), "user-1"))
		w := httptest.NewRecorder()
		h.DeleteUserURLs(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})
}

func TestPingWithoutDB(t *testing.T) {
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

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
			wantStatus: http.StatusNotFound,
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

func TestAuditOnShorten(t *testing.T) {
	rec := &recordAuditor{}
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	const url = "https://practicum.yandex.ru/"
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url))
	r = r.WithContext(auth.WithUserID(r.Context(), "user-42"))
	w := httptest.NewRecorder()

	h.ShortenURL(w, r)
	require.Equal(t, http.StatusCreated, w.Result().StatusCode)

	require.Len(t, rec.events, 1)
	assert.Equal(t, audit.ActionShorten, rec.events[0].Action)
	assert.Equal(t, url, rec.events[0].URL)
	assert.Equal(t, "user-42", rec.events[0].UserID)
	assert.NotZero(t, rec.events[0].TS)
}

func TestAuditOnShortenAPI(t *testing.T) {
	rec := &recordAuditor{}
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	body := `{"url":"https://practicum.yandex.ru/"}`
	w := httptest.NewRecorder()
	h.ShortenAPI(w, httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Result().StatusCode)

	require.Len(t, rec.events, 1)
	assert.Equal(t, audit.ActionShorten, rec.events[0].Action)
	assert.Equal(t, "https://practicum.yandex.ru/", rec.events[0].URL)
}

func TestAuditOnRedirect(t *testing.T) {
	store := storage.NewMemStorage()
	require.NoError(t, store.Save(context.Background(), "testid", "https://practicum.yandex.ru/", ""))

	rec := &recordAuditor{}
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	r := httptest.NewRequest(http.MethodGet, "/testid", nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("id", "testid")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
	w := httptest.NewRecorder()

	h.Redirect(w, r)
	require.Equal(t, http.StatusTemporaryRedirect, w.Result().StatusCode)

	require.Len(t, rec.events, 1)
	assert.Equal(t, audit.ActionFollow, rec.events[0].Action)
	assert.Equal(t, "https://practicum.yandex.ru/", rec.events[0].URL, "в аудит идёт оригинальный, а не сокращённый url")
}

func TestAuditOnShortenConflict(t *testing.T) {
	rec := &recordAuditor{}
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	const url = "https://practicum.yandex.ru/"
	h.ShortenURL(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url)))

	second := httptest.NewRecorder()
	h.ShortenURL(second, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url)))
	require.Equal(t, http.StatusConflict, second.Result().StatusCode)

	require.Len(t, rec.events, 2, "конфликт тоже успешно обслужен и попадает в аудит")
	assert.Equal(t, audit.ActionShorten, rec.events[1].Action)
	assert.Equal(t, url, rec.events[1].URL)
}

func TestNoAuditOnFailedRequests(t *testing.T) {
	store := storage.NewMemStorage()
	rec := &recordAuditor{}
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	h.ShortenURL(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("")))

	missing := httptest.NewRequest(http.MethodGet, "/nosuchid", nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("id", "nosuchid")
	missing = missing.WithContext(context.WithValue(missing.Context(), chi.RouteCtxKey, ctx))
	h.Redirect(httptest.NewRecorder(), missing)

	assert.Empty(t, rec.events, "неуспешные запросы в аудит не попадают")
}

func TestNoAuditOnBatch(t *testing.T) {
	rec := &recordAuditor{}
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, rec, zap.NewNop())

	body := `[{"correlation_id":"1","original_url":"https://practicum.yandex.ru/"}]`
	w := httptest.NewRecorder()
	h.ShortenBatch(w, httptest.NewRequest(http.MethodPost, "/api/shorten/batch", strings.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Result().StatusCode)

	assert.Empty(t, rec.events, "батч в списке аудируемых хэндлеров не значится")
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemStorage()
	require.NoError(t, store.Save(ctx, "id1", "https://example.com/1", "user1"))
	require.NoError(t, store.Save(ctx, "id2", "https://example.com/2", "user2"))

	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
	res := httptest.NewRecorder()
	h.Stats(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "application/json", res.Header().Get("Content-Type"))

	var got models.StatsResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, models.StatsResponse{URLs: 2, Users: 2}, got)
}

func TestStatsStorageFailure(t *testing.T) {
	h := New(failingStore{}, "http://localhost:8080", nil, noopDeleter{}, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
	res := httptest.NewRecorder()
	h.Stats(res, req)

	assert.Equal(t, http.StatusInternalServerError, res.Code)
}
