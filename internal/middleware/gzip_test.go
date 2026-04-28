package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGzipCompressesJSON(t *testing.T) {
	body := `{"result":"http://localhost:8080/abc"}`
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))

	zr, err := gzip.NewReader(res.Body)
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
}

func TestGzipSkipsPlainText(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	assert.Empty(t, res.Header.Get("Content-Encoding"))
	body, _ := io.ReadAll(res.Body)
	assert.Equal(t, "hello", string(body))
}

func TestGzipDecompressesRequest(t *testing.T) {
	original := []byte("hello compressed")

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(original)
	zw.Close()

	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, original, got)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(buf.String()))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Result().StatusCode)
}
