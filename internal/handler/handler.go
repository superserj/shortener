package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type Handler struct {
	store   storage.Repository
	baseURL string
	db      *sql.DB
}

func New(store storage.Repository, baseURL string, db *sql.DB) *Handler {
	return &Handler{
		store:   store,
		baseURL: baseURL,
		db:      db,
	}
}

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
	id := generateID(8)
	status := http.StatusCreated
	if err := h.store.Save(r.Context(), id, originalURL, userID); err != nil {
		var conflict *storage.ConflictError
		if errors.As(err, &conflict) {
			id = conflict.ShortURL
			status = http.StatusConflict
		} else {
			logger.Log.Warn("save failed", zap.Error(err))
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(h.baseURL + "/" + id))
}

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
	id := generateID(8)
	status := http.StatusCreated
	if err := h.store.Save(r.Context(), id, originalURL, userID); err != nil {
		var conflict *storage.ConflictError
		if errors.As(err, &conflict) {
			id = conflict.ShortURL
			status = http.StatusConflict
		} else {
			logger.Log.Warn("save failed", zap.Error(err))
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.ShortenResponse{Result: h.baseURL + "/" + id})
}

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

	items := make([]storage.BatchItem, 0, len(req))
	resp := make([]models.ShortenBatchResponseItem, 0, len(req))

	for _, it := range req {
		original := strings.TrimSpace(it.OriginalURL)
		if original == "" {
			http.Error(w, "empty url in batch", http.StatusBadRequest)
			return
		}
		id := generateID(8)
		items = append(items, storage.BatchItem{ID: id, URL: original})
		resp = append(resp, models.ShortenBatchResponseItem{
			CorrelationID: it.CorrelationID,
			ShortURL:      h.baseURL + "/" + id,
		})
	}

	userID, _ := auth.UserIDFromContext(r.Context())
	if err := h.store.SaveBatch(r.Context(), items, userID); err != nil {
		logger.Log.Warn("save batch failed", zap.Error(err))
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		logger.Log.Info("ping: database not configured")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		logger.Log.Info("ping failed", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	originalURL, err := h.store.Get(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		logger.Log.Warn("get failed", zap.Error(err))
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, originalURL, http.StatusTemporaryRedirect)
}

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

	urls, err := h.store.ListByUser(r.Context(), userID)
	if err != nil {
		logger.Log.Warn("list by user failed", zap.Error(err))
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	if len(urls) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := make([]models.UserURLItem, 0, len(urls))
	for _, u := range urls {
		resp = append(resp, models.UserURLItem{
			ShortURL:    h.baseURL + "/" + u.ShortURL,
			OriginalURL: u.OriginalURL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func generateID(n int) string {
	rngMu.Lock()
	defer rngMu.Unlock()
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}
