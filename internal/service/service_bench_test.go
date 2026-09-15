package service

import "testing"

func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = generateID()
	}
}
