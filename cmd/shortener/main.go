package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/storage"
)

func newRouter(h *handler.Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Get("/{id}", h.Redirect)
	return r
}

func main() {
	cfg := config.New()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		log.Fatal(err)
	}

	store, err := newStore(cfg.FileStoragePath)
	if err != nil {
		log.Fatal(err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	h := handler.New(store, cfg.BaseURL)

	fmt.Println("Starting server on", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, logger.WithLogging(middleware.Gzip(newRouter(h)))); err != nil {
		panic(err)
	}
}

func newStore(path string) (storage.Repository, error) {
	if path == "" {
		return storage.NewMemStorage(), nil
	}
	return storage.NewFileStorage(path)
}
