package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/audit"
	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/cert"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/deleter"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/storage"
)

const (
	shutdownTimeout = 5 * time.Second
	// хэндлеры pprof регистрирует в DefaultServeMux, поэтому держим их на
	// отдельном сервере и только на локальном интерфейсе: наружу профили
	// с содержимым кучи отдавать нельзя
	pprofAddr = "localhost:6060"
)

func newRouter(h *handler.Handler, a *auth.Authenticator, log *zap.Logger) chi.Router {
	r := chi.NewRouter()
	r.Use(logger.WithLogging(log))
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
	printBuildInfo(os.Stdout)

	cfg, err := config.New()
	if err != nil {
		log.Fatal(err)
	}

	lg, err := logger.New(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = lg.Sync() }()

	store, err := newStore(context.Background(), cfg.DatabaseDSN, cfg.FileStoragePath, lg.With(zap.String("component", "storage")))
	if err != nil {
		lg.Fatal("init storage", zap.Error(err))
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	var pinger handler.Pinger
	if p, ok := store.(handler.Pinger); ok {
		pinger = p
	}

	delCtx, delCancel := context.WithCancel(context.Background())
	del := deleter.New(store, lg.With(zap.String("component", "deleter")))
	delDone := make(chan struct{})
	go func() {
		del.Run(delCtx)
		close(delDone)
	}()

	aud, err := newAuditor(cfg, lg.With(zap.String("component", "audit")))
	if err != nil {
		lg.Fatal("init audit", zap.Error(err))
	}
	auditCtx, auditCancel := context.WithCancel(context.Background())
	auditDone := make(chan struct{})
	go func() {
		aud.Run(auditCtx)
		close(auditDone)
	}()

	h := handler.New(store, cfg.BaseURL, pinger, del, aud, lg.With(zap.String("component", "handler")))
	a := auth.New(cfg.AuthSecret)

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: newRouter(h, a, lg)}
	if cfg.EnableHTTPS {
		tlsConfig, err := newTLSConfig(cfg.ServerAddr)
		if err != nil {
			lg.Fatal("init tls", zap.Error(err))
		}
		srv.TLSConfig = tlsConfig
	}

	go func() {
		lg.Info("starting server", zap.String("addr", cfg.ServerAddr), zap.Bool("https", cfg.EnableHTTPS))
		if err := serve(srv, cfg.EnableHTTPS); err != nil && !errors.Is(err, http.ErrServerClosed) {
			lg.Fatal("listen and serve", zap.Error(err))
		}
	}()

	go func() {
		lg.Info("starting pprof server", zap.String("addr", pprofAddr))
		if err := http.ListenAndServe(pprofAddr, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
			lg.Error("pprof listen and serve", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	<-ctx.Done()
	lg.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		lg.Error("server shutdown", zap.Error(err))
	}

	delCancel()
	<-delDone

	auditCancel()
	<-auditDone
}

// serve поднимает сервер в выбранном режиме. Сертификат и ключ для TLS уже
// лежат в srv.TLSConfig, поэтому пути к файлам не нужны.
func serve(srv *http.Server, enableHTTPS bool) error {
	if enableHTTPS {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

// newTLSConfig выпускает самоподписанный сертификат для того адреса, на котором
// поднимается сервер.
func newTLSConfig(addr string) (*tls.Config, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// адрес без порта считаем именем хоста
		host = addr
	}

	certificate, err := cert.Certificate(host)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func newAuditor(cfg *config.Config, log *zap.Logger) (*audit.Auditor, error) {
	aud := audit.New(log)

	if cfg.AuditFile != "" {
		sink, err := audit.NewFileSink(cfg.AuditFile)
		if err != nil {
			return nil, err
		}
		aud.Register(sink)
	}
	if cfg.AuditURL != "" {
		aud.Register(audit.NewHTTPSink(cfg.AuditURL))
	}

	return aud, nil
}

func newStore(ctx context.Context, dsn, path string, log *zap.Logger) (storage.Repository, error) {
	if dsn != "" {
		return storage.NewDBStorage(ctx, dsn)
	}
	if path == "" {
		return storage.NewMemStorage(), nil
	}
	return storage.NewFileStorage(path, log)
}
