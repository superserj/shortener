// Пакет logger даёт общий логгер сервиса и middleware логирования запросов.
package logger

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// New строит продакшн-логгер с указанным уровнем. Логгер передаётся компонентам
// явно — через конструкторы, без глобального состояния: так поведение компонента
// не зависит от скрытой инициализации, а дочерний логгер можно пометить именем
// компонента через log.With(zap.String("component", "...")).
func New(level string) (*zap.Logger, error) {
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return nil, err
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = lvl

	return cfg.Build()
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

// WithLogging возвращает middleware, логирующее метод, URI, статус, размер ответа
// и длительность запроса через переданный логгер.
func WithLogging(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			data := &responseData{status: http.StatusOK}
			lw := &loggingResponseWriter{ResponseWriter: w, data: data}

			next.ServeHTTP(lw, r)

			log.Info("request",
				zap.String("uri", r.RequestURI),
				zap.String("method", r.Method),
				zap.Int("status", data.status),
				zap.Duration("duration", time.Since(start)),
				zap.Int("size", data.size),
			)
		})
	}
}
