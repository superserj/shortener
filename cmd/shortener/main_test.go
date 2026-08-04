package main

import (
	"crypto/tls"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTLSConfig(t *testing.T) {
	cfg, err := newTLSConfig("localhost:8080")
	require.NoError(t, err)
	require.Len(t, cfg.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	cfg, err = newTLSConfig("localhost")
	require.NoError(t, err, "адрес без порта считается именем хоста")
	assert.Len(t, cfg.Certificates, 1)
}

func TestServeStoppedServer(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:0"}
	require.NoError(t, srv.Close())

	assert.True(t, errors.Is(serve(srv, false), http.ErrServerClosed))
	assert.True(t, errors.Is(serve(srv, true), http.ErrServerClosed))
}
