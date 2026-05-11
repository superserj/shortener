package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/logger"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

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

	id := generateID(8)
	h.store.Save(id, originalURL)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
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

	id := generateID(8)
	h.store.Save(id, originalURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.ShortenResponse{Result: h.baseURL + "/" + id})
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

	originalURL, ok := h.store.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, originalURL, http.StatusTemporaryRedirect)
}

func generateID(n int) string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}
