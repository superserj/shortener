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
	for b.Loop() {
		if _, err := s.Get(ctx, "id0005000"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemStorageSave(b *testing.B) {
	ctx := context.Background()
	s := NewMemStorage()

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		// Формирование id/url — подготовка, а не измеряемая работа: исключаем его
		// из тайминга, чтобы бенчмарк отражал только стоимость Save.
		b.StopTimer()
		id := fmt.Sprintf("id%07d", i)
		url := fmt.Sprintf("https://example.com/some/long/path/number/%d/with/tail", i)
		b.StartTimer()
		if err := s.Save(ctx, id, url, "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f"); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkMemStorageListByUser(b *testing.B) {
	s := filledStore(b)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.ListByUser(ctx, "cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f"); err != nil {
			b.Fatal(err)
		}
	}
}
