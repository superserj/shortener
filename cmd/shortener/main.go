package main

import (
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var urlStore = make(map[string]string)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateID(n int) string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

func shortenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST is allowed", http.StatusBadRequest)
		return
	}

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
	urlStore[id] = originalURL

	shortURL := "http://localhost:8080/" + id

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(shortURL))
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET is allowed", http.StatusBadRequest)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	originalURL, ok := urlStore[id]
	if !ok {
		http.Error(w, "not found", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, originalURL, http.StatusTemporaryRedirect)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodPost {
			shortenHandler(w, r)
			return
		}
		redirectHandler(w, r)
	})

	if err := http.ListenAndServe(":8080", mux); err != nil {
		panic(err)
	}
}
