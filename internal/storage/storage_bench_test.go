package storage

import (
	"context"
	"fmt"
	"testing"
)

const benchStoreSize = 10000

func filledStore(b *testing.B) *MemStorage {
	b.Helper()
	s := NewMemStorage()
	for i := 0; i < benchStoreSize; i++ {
		id := fmt.Sprintf("id%07d", i)
		url := fmt.Sprintf("https://example.com/some/long/path/number/%d/with/tail", i)
		if err := s.Save(context.Background(), id, url, "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f"); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

func BenchmarkMemStorageGet(b *testing.B) {
	s := filledStore(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(ctx, "id0005000"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemStorageSave(b *testing.B) {
	ctx := context.Background()
	s := NewMemStorage()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("id%07d", i)
		url := fmt.Sprintf("https://example.com/some/long/path/number/%d/with/tail", i)
		if err := s.Save(ctx, id, url, "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemStorageListByUser(b *testing.B) {
	s := filledStore(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListByUser(ctx, "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f"); err != nil {
			b.Fatal(err)
		}
	}
}
