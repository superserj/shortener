// Пакет models описывает форматы тел запросов и ответов API.
package models

// ShortenRequest — тело запроса POST /api/shorten.
type ShortenRequest struct {
	URL string `json:"url"`
}

// ShortenResponse — тело ответа POST /api/shorten.
type ShortenResponse struct {
	Result string `json:"result"`
}

// ShortenBatchRequestItem — элемент тела запроса POST /api/shorten/batch.
type ShortenBatchRequestItem struct {
	CorrelationID string `json:"correlation_id"`
	OriginalURL   string `json:"original_url"`
}

// ShortenBatchResponseItem — элемент тела ответа POST /api/shorten/batch.
type ShortenBatchResponseItem struct {
	CorrelationID string `json:"correlation_id"`
	ShortURL      string `json:"short_url"`
}

// UserURLItem — элемент тела ответа GET /api/user/urls.
type UserURLItem struct {
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}
