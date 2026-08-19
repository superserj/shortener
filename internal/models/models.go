// Пакет models описывает форматы тел запросов и ответов API.
package models

//go:generate go run github.com/superserj/shortener/cmd/reset -dir ../..

// ShortenRequest — тело запроса POST /api/shorten.
type ShortenRequest struct {
	URL string `json:"url"`
}

// ShortenResponse — тело ответа POST /api/shorten.
type ShortenResponse struct {
	Result string `json:"result"`
}

// ShortenBatchRequestItem — элемент тела запроса POST /api/shorten/batch.
// generate:reset
type ShortenBatchRequestItem struct {
	CorrelationID string `json:"correlation_id"`
	OriginalURL   string `json:"original_url"`
}

// ShortenBatchResponseItem — элемент тела ответа POST /api/shorten/batch.
// generate:reset
type ShortenBatchResponseItem struct {
	CorrelationID string `json:"correlation_id"`
	ShortURL      string `json:"short_url"`
}

// UserURLItem — элемент тела ответа GET /api/user/urls.
// generate:reset
type UserURLItem struct {
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

// StatsResponse — тело ответа GET /api/internal/stats.
type StatsResponse struct {
	URLs  int `json:"urls"`
	Users int `json:"users"`
}
