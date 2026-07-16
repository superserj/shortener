package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/storage"
)

func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateID(8)
	}
}

func BenchmarkShortenURL(b *testing.B) {
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, nil)
	ctx := auth.WithUserID(context.Background(), "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := fmt.Sprintf("https://example.com/some/long/path/number/%d/with/tail", i)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(ctx)
		h.ShortenURL(httptest.NewRecorder(), r)
	}
}

func BenchmarkShortenAPI(b *testing.B) {
	h := New(storage.NewMemStorage(), "http://localhost:8080", nil, noopDeleter{}, nil)
	ctx := auth.WithUserID(context.Background(), "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := fmt.Sprintf(`{"url":"https://example.com/some/long/path/number/%d/with/tail"}`, i)
		r := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body)).WithContext(ctx)
		h.ShortenAPI(httptest.NewRecorder(), r)
	}
}

func BenchmarkRedirect(b *testing.B) {
	store := storage.NewMemStorage()
	if err := store.Save(context.Background(), "benchid", "https://example.com/target", "u1"); err != nil {
		b.Fatal(err)
	}
	h := New(store, "http://localhost:8080", nil, noopDeleter{}, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "benchid")
	r := httptest.NewRequest(http.MethodGet, "/benchid", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Redirect(httptest.NewRecorder(), r)
	}
}
