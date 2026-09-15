package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/middleware"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/pb"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

type noopDeleter struct{}

func (noopDeleter) Enqueue(_ string, _ []string) {}

func TestNewStore(t *testing.T) {
	ctx := context.Background()
	log := zap.NewNop()

	mem, err := newStore(ctx, "", "", log)
	require.NoError(t, err)
	assert.IsType(t, &storage.MemStorage{}, mem)

	file, err := newStore(ctx, "", filepath.Join(t.TempDir(), "urls.json"), log)
	require.NoError(t, err)
	require.IsType(t, &storage.FileStorage{}, file)
	require.NoError(t, file.(*storage.FileStorage).Close())
}

func TestNewAuditor(t *testing.T) {
	dir := t.TempDir()

	aud, err := newAuditor(&config.Config{}, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, aud, "аудит без приёмников тоже создаётся")

	aud, err = newAuditor(&config.Config{
		AuditFile: filepath.Join(dir, "audit.log"),
		AuditURL:  "http://localhost:8181/audit",
	}, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, aud)

	_, err = newAuditor(&config.Config{AuditFile: filepath.Join(dir, "missing", "audit.log")}, zap.NewNop())
	assert.Error(t, err, "недоступный файл аудита останавливает запуск")
}

func TestNewTLSConfig(t *testing.T) {
	cfg, err := newTLSConfig("localhost:8080")
	require.NoError(t, err)
	require.Len(t, cfg.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	cfg, err = newTLSConfig("localhost")
	require.NoError(t, err, "адрес без порта считается именем хоста")
	assert.Len(t, cfg.Certificates, 1)

	_, err = newTLSConfig("localhost:8080:9090")
	assert.Error(t, err, "разбитый адрес не должен превращаться в имя хоста")
}

func TestServeStoppedServer(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:0"}
	require.NoError(t, srv.Close())

	assert.True(t, errors.Is(serve(srv, false), http.ErrServerClosed))
	assert.True(t, errors.Is(serve(srv, true), http.ErrServerClosed))
}

func TestNewRouterRoutes(t *testing.T) {
	store := storage.NewMemStorage()
	log := zap.NewNop()
	h := handler.New(service.New(store, "http://localhost:8080", nil), nil, noopDeleter{}, log)
	trusted, err := middleware.TrustedSubnet("")
	require.NoError(t, err)
	require.Nil(t, trusted, "без подсети мидлварь не создаётся")
	r := newRouter(h, auth.New("test-secret"), trusted, log)

	srv := httptest.NewServer(r)
	defer srv.Close()

	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	res, err := client.Post(srv.URL+"/", "text/plain", strings.NewReader("https://practicum.yandex.ru/"))
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusCreated, res.StatusCode)

	res, err = client.Get(srv.URL + "/unknown1")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	res, err = client.Get(srv.URL + "/ping")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode, "без базы ping отвечает ошибкой")

	res, err = client.Get(srv.URL + "/api/internal/stats")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "без доверенной подсети статистика закрыта")
}

func TestNewRouterStats(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemStorage()
	require.NoError(t, store.Save(ctx, "id1", "https://practicum.yandex.ru/", "user1"))

	log := zap.NewNop()
	h := handler.New(service.New(store, "http://localhost:8080", nil), nil, noopDeleter{}, log)
	trusted, err := middleware.TrustedSubnet("127.0.0.0/8")
	require.NoError(t, err)

	srv := httptest.NewServer(newRouter(h, auth.New("test-secret"), trusted, log))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/internal/stats", nil)
	require.NoError(t, err)
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "без заголовка с адресом доступа нет")

	req.Header.Set("X-Real-IP", "127.0.0.1")
	res, err = srv.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var stats models.StatsResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&stats))
	assert.Equal(t, models.StatsResponse{URLs: 1, Users: 1}, stats)
}

func TestNewGRPCServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	svc := service.New(storage.NewMemStorage(), "http://localhost:8080", nil)
	srv := newGRPCServer(svc, auth.New("test-secret"), nil, zap.NewNop())
	go func() {
		_ = srv.Serve(listener)
	}()
	defer srv.GracefulStop()

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewShortenerServiceClient(conn)
	resp, err := client.ShortenURL(context.Background(),
		pb.URLShortenRequest_builder{Url: proto.String("https://practicum.yandex.ru/")}.Build())
	require.NoError(t, err)
	assert.Contains(t, resp.GetResult(), "http://localhost:8080/")
}
