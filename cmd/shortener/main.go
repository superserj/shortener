package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/deleter"
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
	r.Delete("/api/user/urls", h.DeleteUserURLs)
	r.Get("/ping", h.Ping)
	r.Get("/{id}", h.Redirect)
	return r
}

func main() {
	cfg := config.New()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		log.Fatal(err)
	}

	store, err := newStore(context.Background(), cfg.DatabaseDSN, cfg.FileStoragePath)
	if err != nil {
		logger.Log.Fatal("init storage", zap.Error(err))
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	var pinger handler.Pinger
	if p, ok := store.(handler.Pinger); ok {
		pinger = p
	}

	delCtx, delCancel := context.WithCancel(context.Background())
	del := deleter.New(store)
	delDone := make(chan struct{})
	go func() {
		del.Run(delCtx)
		close(delDone)
	}()

	h := handler.New(store, cfg.BaseURL, pinger, del)
	a := auth.New(cfg.AuthSecret)

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: newRouter(h, a)}

	go func() {
		logger.Log.Info("starting server", zap.String("addr", cfg.ServerAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Fatal("listen and serve", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("server shutdown", zap.Error(err))
	}

	delCancel()
	<-delDone
}

func newStore(ctx context.Context, dsn, path string) (storage.Repository, error) {
	if dsn != "" {
		return storage.NewDBStorage(ctx, dsn)
	}
	if path == "" {
		return storage.NewMemStorage(), nil
	}
	return storage.NewFileStorage(path)
}
