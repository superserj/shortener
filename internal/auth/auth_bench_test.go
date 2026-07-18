package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkSign(b *testing.B) {
	a := New("bench-secret")

	b.ReportAllocs()
	for b.Loop() {
		_ = a.Sign("cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")
	}
}

func BenchmarkVerify(b *testing.B) {
	a := New("bench-secret")
	signed := a.Sign("cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")

	b.ReportAllocs()
	for b.Loop() {
		if _, err := a.Verify(signed); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMiddleware(b *testing.B) {
	a := New("bench-secret")
	next := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: a.Sign("cd1a3f5e7b9d2c4a6e8f0b1d3a5c7e9f")})

	b.ReportAllocs()
	for b.Loop() {
		next.ServeHTTP(httptest.NewRecorder(), r)
	}
}
