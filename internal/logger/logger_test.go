package logger

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewLevels(t *testing.T) {
	log, err := New("debug")
	require.NoError(t, err)
	assert.True(t, log.Core().Enabled(zapcore.DebugLevel))

	log, err = New("error")
	require.NoError(t, err)
	assert.False(t, log.Core().Enabled(zapcore.InfoLevel))

	_, err = New("loud")
	assert.Error(t, err)
}

func TestWithLoggingRecordsRequest(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	handler := WithLogging(zap.New(core))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/shorten", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "/api/shorten", fields["uri"])
	assert.Equal(t, http.MethodPost, fields["method"])
	assert.Equal(t, int64(http.StatusCreated), fields["status"])
	assert.Equal(t, int64(len("hello")), fields["size"])
	assert.Contains(t, fields, "duration")
}

func TestWithLoggingDefaultStatus(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	handler := WithLogging(zap.New(core))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, int64(http.StatusOK), logs.All()[0].ContextMap()["status"],
		"обработчик без WriteHeader логируется как 200")
}
