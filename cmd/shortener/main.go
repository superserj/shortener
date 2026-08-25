package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/superserj/shortener/internal/audit"
	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/cert"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/deleter"
	"github.com/superserj/shortener/internal/grpcapi"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/pb"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

const (
	shutdownTimeout = 5 * time.Second
	// хэндлеры pprof регистрирует в DefaultServeMux, поэтому держим их на
	// отдельном сервере и только на локальном интерфейсе: наружу профили
	// с содержимым кучи отдавать нельзя
	pprofAddr = "localhost:6060"
	// errMissingPort — текст ошибки net.SplitHostPort для адреса, в котором
	// нет порта: единственный случай, когда адрес можно принять как есть
	errMissingPort = "missing port in address"
)

// newRouter собирает маршруты сервиса. Обработчик статистики закрыт отдельной
// мидлварью: она пускает к нему только запросы из доверенной подсети.
func newRouter(h *handler.Handler, a *auth.Authenticator, trusted func(http.Handler) http.Handler, log *zap.Logger) chi.Router {
	r := chi.NewRouter()
	r.Use(logger.WithLogging(log))
	r.Use(middleware.Gzip)
	r.Use(a.Middleware)
	r.Post("/", h.ShortenURL)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Post("/api/shorten/batch", h.ShortenBatch)
	r.Get("/api/user/urls", h.UserURLs)
	r.Delete("/api/user/urls", h.DeleteUserURLs)
	r.With(trusted).Get("/api/internal/stats", h.Stats)
	r.Get("/ping", h.Ping)
	r.Get("/{id}", h.Redirect)
	return r
}

func main() {
	printBuildInfo(os.Stdout)

	cfg, err := config.New()
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}

	lg, err := logger.New(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = lg.Sync() }()

	// TLS готовим до подъёма хранилища и фоновых воркеров: выход по ошибке
	// на этом шаге не должен обрывать уже запущенные компоненты
	var tlsConfig *tls.Config
	if cfg.EnableHTTPS {
		tlsConfig, err = newTLSConfig(cfg.ServerAddr)
		if err != nil {
			lg.Fatal("init tls", zap.Error(err))
		}
	}

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

	svc := service.New(store, cfg.BaseURL, aud)
	h := handler.New(svc, pinger, del, lg.With(zap.String("component", "handler")))
	a := auth.New(cfg.AuthSecret)

	trusted, err := middleware.TrustedSubnet(cfg.TrustedSubnet)
	if err != nil {
		lg.Fatal("init trusted subnet", zap.Error(err))
	}

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: newRouter(h, a, trusted, lg), TLSConfig: tlsConfig}

	// gRPC поднимается только с заданным адресом: пустая настройка оставляет
	// сервис таким же, каким он был до появления второго транспорта
	var grpcSrv *grpc.Server
	if cfg.GRPCAddr != "" {
		listener, listenErr := net.Listen("tcp", cfg.GRPCAddr)
		if listenErr != nil {
			lg.Fatal("listen grpc", zap.Error(listenErr))
		}
		grpcSrv = newGRPCServer(svc, a, tlsConfig, lg.With(zap.String("component", "grpc")))
		go func() {
			lg.Info("starting grpc server", zap.String("addr", cfg.GRPCAddr), zap.Bool("tls", tlsConfig != nil))
			if err := grpcSrv.Serve(listener); err != nil {
				lg.Error("grpc serve", zap.Error(err))
			}
		}()
	}

	go func() {
		lg.Info("starting server", zap.String("addr", cfg.ServerAddr), zap.Bool("https", cfg.EnableHTTPS))
		if err := serve(srv, cfg.EnableHTTPS); err != nil && !errors.Is(err, http.ErrServerClosed) {
			lg.Fatal("listen and serve", zap.Error(err))
		}
	}()

	pprofSrv := &http.Server{Addr: pprofAddr}
	go func() {
		lg.Info("starting pprof server", zap.String("addr", pprofAddr))
		if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			lg.Error("pprof listen and serve", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	<-ctx.Done()
	// возвращаем сигналам поведение по умолчанию: повторный сигнал во время
	// остановки должен прерывать процесс, а не теряться
	stop()
	lg.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		lg.Error("server shutdown", zap.Error(err))
	}
	if err := pprofSrv.Shutdown(shutdownCtx); err != nil {
		lg.Error("pprof server shutdown", zap.Error(err))
	}
	if grpcSrv != nil {
		stopGRPC(shutdownCtx, grpcSrv)
	}

	delCancel()
	<-delDone

	auditCancel()
	<-auditDone
}

// stopGRPC останавливает gRPC-сервер, дожидаясь начатых вызовов. Ожидание
// ограничено общим сроком остановки: зависший вызов не должен держать процесс,
// которому уже пришёл сигнал.
func stopGRPC(ctx context.Context, srv *grpc.Server) {
	stopped := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		srv.Stop()
		<-stopped
	}
}

// newGRPCServer собирает gRPC-сервер с теми же зависимостями, что и HTTP:
// перехватчики повторяют мидлвари логирования и аутентификации, а с включённым
// HTTPS соединения защищает тот же самоподписанный сертификат.
func newGRPCServer(svc *service.Service, a *auth.Authenticator, tlsConfig *tls.Config, log *zap.Logger) *grpc.Server {
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			grpcapi.LoggingInterceptor(log),
			grpcapi.AuthInterceptor(a, log),
		),
	}
	if tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	srv := grpc.NewServer(opts...)
	pb.RegisterShortenerServiceServer(srv, grpcapi.NewServer(svc, log))
	return srv
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
		// адрес без порта считаем именем хоста, а вот разбитый адрес молча
		// принимать нельзя: сертификат выпишется на мусорное имя, и проверка
		// у клиента упадёт уже во время работы
		var addrErr *net.AddrError
		if !errors.As(err, &addrErr) || addrErr.Err != errMissingPort {
			return nil, fmt.Errorf("parse server address %q: %w", addr, err)
		}
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
