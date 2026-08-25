package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
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
	h := handler.New(store, "http://localhost:8080", nil, noopDeleter{}, nil, log)
	r := newRouter(h, auth.New("test-secret"), log)

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
}
