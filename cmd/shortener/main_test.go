package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortenHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
	}{
		{
			name:       "valid url",
			method:     http.MethodPost,
			body:       "https://practicum.yandex.ru/",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "empty body",
			method:     http.MethodPost,
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       "https://practicum.yandex.ru/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			shortenHandler(w, r)

			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			if tt.wantStatus == http.StatusCreated {
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), "http://localhost:8080/")
				assert.Equal(t, "text/plain", res.Header.Get("Content-Type"))
			}
		})
	}
}

func TestRedirectHandler(t *testing.T) {
	urlStore["testid"] = "https://practicum.yandex.ru/"

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantURL    string
	}{
		{
			name:       "existing id",
			method:     http.MethodGet,
			path:       "/testid",
			wantStatus: http.StatusTemporaryRedirect,
			wantURL:    "https://practicum.yandex.ru/",
		},
		{
			name:       "missing id",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty id",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "wrong method",
			method:     http.MethodPost,
			path:       "/testid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			redirectHandler(w, r)

			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			if tt.wantStatus == http.StatusTemporaryRedirect {
				assert.Equal(t, tt.wantURL, res.Header.Get("Location"))
			}
		})
	}
}
