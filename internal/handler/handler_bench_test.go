package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

func BenchmarkShortenURL(b *testing.B) {
	h := New(service.New(storage.NewMemStorage(), "http://localhost:8080", nil), nil, noopDeleter{}, zap.NewNop())
	ctx := auth.WithUserID(context.Background(), "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		// Тело и запрос строим вне тайминга: измеряем только обработку в хендлере.
		b.StopTimer()
		body := fmt.Sprintf("https://example.com/some/long/path/number/%d/with/tail", i)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(ctx)
		b.StartTimer()
		h.ShortenURL(httptest.NewRecorder(), r)
		i++
	}
}

func BenchmarkShortenAPI(b *testing.B) {
	h := New(service.New(storage.NewMemStorage(), "http://localhost:8080", nil), nil, noopDeleter{}, zap.NewNop())
	ctx := auth.WithUserID(context.Background(), "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		b.StopTimer()
		body := fmt.Sprintf(`{"url":"https://example.com/some/long/path/number/%d/with/tail"}`, i)
		r := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body)).WithContext(ctx)
		b.StartTimer()
		h.ShortenAPI(httptest.NewRecorder(), r)
		i++
	}
}

func BenchmarkRedirect(b *testing.B) {
	store := storage.NewMemStorage()
	if err := store.Save(context.Background(), "benchid", "https://example.com/target", "u1"); err != nil {
		b.Fatal(err)
	}
	h := New(service.New(store, "http://localhost:8080", nil), nil, noopDeleter{}, zap.NewNop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "benchid")
	r := httptest.NewRequest(http.MethodGet, "/benchid", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	b.ReportAllocs()
	for b.Loop() {
		h.Redirect(httptest.NewRecorder(), r)
	}
}
