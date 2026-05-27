package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/storage"
)

func newRouter(h *handler.Handler, a *auth.Authenticator) chi.Router {
	r := chi.NewRouter()
	r.Use(logger.WithLogging)
	r.Use(middleware.Gzip)
	r.Use(a.Middleware)
	r.Post("/", h.ShortenURL)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Post("/api/shorten/batch", h.ShortenBatch)
	r.Get("/api/user/urls", h.UserURLs)
	r.Get("/ping", h.Ping)
	r.Get("/{id}", h.Redirect)
	return r
}

func main() {
	cfg := config.New()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		log.Fatal(err)
	}

	db, err := newDB(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}

	store, err := newStore(db, cfg.FileStoragePath)
	if err != nil {
		log.Fatal(err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	h := handler.New(store, cfg.BaseURL, db)
	a := auth.New(cfg.AuthSecret)

	fmt.Println("Starting server on", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, newRouter(h, a)); err != nil {
		panic(err)
	}
}

func newStore(db *sql.DB, path string) (storage.Repository, error) {
	if db != nil {
		return storage.NewDBStorage(db)
	}
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
