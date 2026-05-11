package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/storage"
)

func newRouter(h *handler.Handler) chi.Router {
	r := chi.NewRouter()
	r.Use(logger.WithLogging)
	r.Use(middleware.Gzip)
	r.Post("/", h.ShortenURL)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Get("/ping", h.Ping)
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

	db, err := newDB(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}

	h := handler.New(store, cfg.BaseURL, db)

	fmt.Println("Starting server on", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, newRouter(h)); err != nil {
		panic(err)
	}
}

func newStore(path string) (storage.Repository, error) {
	if path == "" {
		return storage.NewMemStorage(), nil
	}
	return storage.NewFileStorage(path)
}

func newDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, nil
	}
	return sql.Open("postgres", dsn)
}
