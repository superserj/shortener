// Пакет handler содержит HTTP-обработчики сервиса сокращения ссылок. Сама
// логика работы со ссылками живёт в пакете service, общем для HTTP и gRPC.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

// DeleteEnqueuer принимает короткие ссылки на асинхронное удаление.
type DeleteEnqueuer interface {
	Enqueue(userID string, ids []string)
}

// Pinger проверяет доступность хранилища.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler обслуживает эндпоинты сервиса.
type Handler struct {
	svc     *service.Service
	pinger  Pinger
	deleter DeleteEnqueuer
	log     *zap.Logger
}

// New создаёт обработчик. Логгер передаётся явно. Аргумент pinger может быть
// nil: тогда эндпоинт проверки БД отвечает ошибкой.
func New(svc *service.Service, pinger Pinger, deleter DeleteEnqueuer, log *zap.Logger) *Handler {
	return &Handler{
		svc:     svc,
		pinger:  pinger,
		deleter: deleter,
		log:     log,
	}
}

// ShortenURL обслуживает POST / — принимает оригинальный адрес текстом и
// возвращает короткую ссылку. Отвечает 201, а если адрес уже сокращали — 409
// с ранее выданной ссылкой.
func (h *Handler) ShortenURL(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	originalURL := strings.TrimSpace(string(body))
	if originalURL == "" {
		http.Error(w, "empty url", http.StatusBadRequest)
		return
	}

	userID, _ := auth.UserIDFromContext(r.Context())
	shortURL, conflict, err := h.svc.Shorten(r.Context(), originalURL, userID)
	if err != nil {
		h.log.Warn("save failed", zap.Error(err))
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(shortenStatus(conflict))
	w.Write([]byte(shortURL))
}

// ShortenAPI обслуживает POST /api/shorten — то же, что ShortenURL, но принимает
// и возвращает JSON.
func (h *Handler) ShortenAPI(w http.ResponseWriter, r *http.Request) {
	var req models.ShortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	originalURL := strings.TrimSpace(req.URL)
	if originalURL == "" {
		http.Error(w, "empty url", http.StatusBadRequest)
		return
	}

	userID, _ := auth.UserIDFromContext(r.Context())
	shortURL, conflict, err := h.svc.Shorten(r.Context(), originalURL, userID)
	if err != nil {
		h.log.Warn("save failed", zap.Error(err))
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(shortenStatus(conflict))
	json.NewEncoder(w).Encode(models.ShortenResponse{Result: shortURL})
}

// ShortenBatch обслуживает POST /api/shorten/batch — сокращает пачку адресов
// за один запрос, сохраняя соответствие по correlation_id.
func (h *Handler) ShortenBatch(w http.ResponseWriter, r *http.Request) {
	var req []models.ShortenBatchRequestItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req) == 0 {
		http.Error(w, "empty batch", http.StatusBadRequest)
		return
	}

	urls := make([]string, 0, len(req))
	for _, it := range req {
		original := strings.TrimSpace(it.OriginalURL)
		if original == "" {
			http.Error(w, "empty url in batch", http.StatusBadRequest)
			return
		}
		urls = append(urls, original)
	}

	userID, _ := auth.UserIDFromContext(r.Context())
	shortURLs, err := h.svc.ShortenBatch(r.Context(), urls, userID)
	if err != nil {
		h.log.Warn("save batch failed", zap.Error(err))
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	// ответ собираем по сохранённым ссылкам: для адреса, который уже сокращали,
	// хранилище возвращает выданную ранее ссылку, а не сгенерированную сейчас
	resp := make([]models.ShortenBatchResponseItem, 0, len(shortURLs))
	for i, shortURL := range shortURLs {
		resp = append(resp, models.ShortenBatchResponseItem{
			CorrelationID: req[i].CorrelationID,
			ShortURL:      shortURL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Ping обслуживает GET /ping — проверяет соединение с базой данных.
func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	if h.pinger == nil {
		h.log.Info("ping: database not configured")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()

	if err := h.pinger.Ping(ctx); err != nil {
		h.log.Info("ping failed", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Redirect обслуживает GET /{id} — отправляет на оригинальный адрес ответом 307.
// Для удалённой ссылки отвечает 410, для неизвестной — 404.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	userID, _ := auth.UserIDFromContext(r.Context())
	originalURL, err := h.svc.Expand(r.Context(), id, userID)
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, storage.ErrDeleted) {
		http.Error(w, "gone", http.StatusGone)
		return
	}
	if err != nil {
		h.log.Warn("get failed", zap.Error(err))
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, originalURL, http.StatusTemporaryRedirect)
}

// UserURLs обслуживает GET /api/user/urls — отдаёт ссылки текущего пользователя.
// Если их нет, отвечает 204.
func (h *Handler) UserURLs(w http.ResponseWriter, r *http.Request) {
	if auth.CookieInvalidFromContext(r.Context()) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	urls, err := h.svc.ListUserURLs(r.Context(), userID)
	if err != nil {
		h.log.Warn("list by user failed", zap.Error(err))
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	if len(urls) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(urls)
}

// Stats обслуживает GET /api/internal/stats — отдаёт количество сокращённых
// адресов и пользователей сервиса. Доступ к эндпоинту ограничен доверенной
// подсетью, сам обработчик проверок не делает.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.Stats(r.Context())
	if err != nil {
		h.log.Warn("stats failed", zap.Error(err))
		http.Error(w, "stats failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(models.StatsResponse{URLs: stats.URLs, Users: stats.Users})
}

// DeleteUserURLs обслуживает DELETE /api/user/urls — принимает ссылки на
// удаление и сразу отвечает 202, удаляя их в фоне.
func (h *Handler) DeleteUserURLs(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	h.deleter.Enqueue(userID, ids)
	w.WriteHeader(http.StatusAccepted)
}

// shortenStatus выбирает код ответа: адрес, который уже сокращали, отдаётся
// с 409, новый — с 201.
func shortenStatus(conflict bool) int {
	if conflict {
		return http.StatusConflict
	}
	return http.StatusCreated
}
