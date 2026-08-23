package grpcapi

import (
	"context"
	"errors"

	"github.com/superserj/shortener/internal/storage"
)

// failingStore подставляется вместо хранилища, когда нужно проверить, каким
// кодом служба отвечает на его ошибки.
type failingStore struct{}

var errStoreUnavailable = errors.New("storage unavailable")

func (failingStore) Save(_ context.Context, _, _, _ string) error { return errStoreUnavailable }

func (failingStore) SaveBatch(_ context.Context, _ []storage.BatchItem, _ string) ([]storage.BatchItem, error) {
	return nil, errStoreUnavailable
}

func (failingStore) Get(_ context.Context, _ string) (string, error) { return "", errStoreUnavailable }

func (failingStore) ListByUser(_ context.Context, _ string) ([]storage.UserURL, error) {
	return nil, errStoreUnavailable
}

func (failingStore) MarkDeleted(_ context.Context, _ string, _ []string) error {
	return errStoreUnavailable
}

func (failingStore) Stats(_ context.Context) (storage.Stats, error) {
	return storage.Stats{}, errStoreUnavailable
}
