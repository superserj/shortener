package main

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superserj/shortener/internal/config"
	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/storage"
)

func newRouter(h *handler.Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Get("/{id}", h.Redirect)
	return r
}

func main() {
	cfg := config.New()

	store := storage.NewMemStorage()
	h := handler.New(store, cfg.BaseURL)

	fmt.Println("Starting server on", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, newRouter(h)); err != nil {
		panic(err)
	}
}
