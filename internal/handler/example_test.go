package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/superserj/shortener/internal/handler"
	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/storage"
)

const baseURL = "http://localhost:8080"

type noopDeleter struct{}

func (noopDeleter) Enqueue(_ string, _ []string) {}

// newServer поднимает сервис с хранилищем в памяти.
func newServer() *httptest.Server {
	store := storage.NewMemStorage()
	h := handler.New(store, baseURL, nil, noopDeleter{}, nil, zap.NewNop())

	r := chi.NewRouter()
	r.Post("/", h.ShortenURL)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Post("/api/shorten/batch", h.ShortenBatch)
	r.Get("/{id}", h.Redirect)

	return httptest.NewServer(r)
}

// Сокращение адреса через POST /: тело запроса и ответа — простой текст.
func ExampleHandler_ShortenURL() {
	srv := newServer()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader("https://practicum.yandex.ru/"))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode)
	fmt.Println(strings.HasPrefix(string(body), baseURL+"/"))

	// Output:
	// 201
	// true
}

// Повторное сокращение того же адреса отвечает 409 и возвращает выданную ранее ссылку.
func ExampleHandler_ShortenURL_conflict() {
	srv := newServer()
	defer srv.Close()

	const target = "https://practicum.yandex.ru/"

	first, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader(target))
	if err != nil {
		fmt.Println(err)
		return
	}
	firstBody, _ := io.ReadAll(first.Body)
	first.Body.Close()

	second, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader(target))
	if err != nil {
		fmt.Println(err)
		return
	}
	secondBody, _ := io.ReadAll(second.Body)
	second.Body.Close()

	fmt.Println(second.StatusCode)
	fmt.Println(string(firstBody) == string(secondBody))

	// Output:
	// 409
	// true
}

// Сокращение адреса через POST /api/shorten: тело запроса и ответа — JSON.
func ExampleHandler_ShortenAPI() {
	srv := newServer()
	defer srv.Close()

	body, err := json.Marshal(models.ShortenRequest{URL: "https://practicum.yandex.ru/"})
	if err != nil {
		fmt.Println(err)
		return
	}

	resp, err := http.Post(srv.URL+"/api/shorten", "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	var result models.ShortenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(resp.StatusCode)
	fmt.Println(strings.HasPrefix(result.Result, baseURL+"/"))

	// Output:
	// 201
	// true
}

// Сокращение пачки адресов через POST /api/shorten/batch: каждому элементу
// ответа соответствует correlation_id из запроса.
func ExampleHandler_ShortenBatch() {
	srv := newServer()
	defer srv.Close()

	body, err := json.Marshal([]models.ShortenBatchRequestItem{
		{CorrelationID: "first", OriginalURL: "https://practicum.yandex.ru/"},
		{CorrelationID: "second", OriginalURL: "https://go.dev/"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	resp, err := http.Post(srv.URL+"/api/shorten/batch", "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	var result []models.ShortenBatchResponseItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(resp.StatusCode)
	for _, item := range result {
		fmt.Println(item.CorrelationID, strings.HasPrefix(item.ShortURL, baseURL+"/"))
	}

	// Output:
	// 201
	// first true
	// second true
}

// Переход по короткой ссылке через GET /{id}: сервис отвечает редиректом 307.
func ExampleHandler_Redirect() {
	srv := newServer()
	defer srv.Close()

	const target = "https://practicum.yandex.ru/"

	created, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader(target))
	if err != nil {
		fmt.Println(err)
		return
	}
	short, _ := io.ReadAll(created.Body)
	created.Body.Close()

	// короткая ссылка выдана с базовым адресом сервиса, обращаемся к тестовому
	id := strings.TrimPrefix(string(short), baseURL)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(srv.URL + id)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	fmt.Println(resp.StatusCode)
	fmt.Println(resp.Header.Get("Location"))

	// Output:
	// 307
	// https://practicum.yandex.ru/
}

// Запрос неизвестной короткой ссылки отвечает 404.
func ExampleHandler_Redirect_notFound() {
	srv := newServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nosuchid")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	fmt.Println(resp.StatusCode)

	// Output:
	// 404
}

// Хранилище можно наполнить и напрямую, минуя HTTP.
func ExampleNew() {
	store := storage.NewMemStorage()
	if err := store.Save(context.Background(), "abc12345", "https://practicum.yandex.ru/", "user-1"); err != nil {
		fmt.Println(err)
		return
	}

	h := handler.New(store, baseURL, nil, noopDeleter{}, nil, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/{id}", h.Redirect)
	srv := httptest.NewServer(r)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(srv.URL + "/abc12345")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	fmt.Println(resp.Header.Get("Location"))

	// Output:
	// https://practicum.yandex.ru/
}
