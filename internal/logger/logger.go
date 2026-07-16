// Пакет logger даёт общий логгер сервиса и middleware логирования запросов.
package logger

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Log — общий логгер сервиса. До вызова Initialize ничего не пишет.
var Log *zap.Logger = zap.NewNop()

// Initialize настраивает Log на указанный уровень логирования.
func Initialize(level string) error {
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return err
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = lvl

	zl, err := cfg.Build()
	if err != nil {
		return err
	}

	Log = zl
	return nil
}

type responseData struct {
	status int
	size   int
}

type loggingResponseWriter struct {
	http.ResponseWriter
	data *responseData
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.data.size += n
	return n, err
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	w.data.status = statusCode
}

// WithLogging логирует метод, URI, статус, размер ответа и длительность запроса.
func WithLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		data := &responseData{status: http.StatusOK}
		lw := &loggingResponseWriter{ResponseWriter: w, data: data}

		next.ServeHTTP(lw, r)

		Log.Info("request",
			zap.String("uri", r.RequestURI),
			zap.String("method", r.Method),
			zap.Int("status", data.status),
			zap.Duration("duration", time.Since(start)),
			zap.Int("size", data.size),
		)
	})
}
