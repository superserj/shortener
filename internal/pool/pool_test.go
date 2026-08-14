package pool_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/superserj/shortener/internal/models"
	"github.com/superserj/shortener/internal/pool"
)

// buffer — объект со счётчиком сбросов: он показывает, что Put действительно
// очищает состояние, не полагаясь на то, вернёт ли sync.Pool тот же указатель.
type buffer struct {
	data   []byte
	resets int
}

func (b *buffer) Reset() {
	b.data = b.data[:0]
	b.resets++
}

func TestGetReturnsUsableObject(t *testing.T) {
	p := pool.New[buffer]()

	obj := p.Get()
	require.NotNil(t, obj)
	assert.Empty(t, obj.data)
	assert.Zero(t, obj.resets)
}

func TestPutResetsObject(t *testing.T) {
	p := pool.New[buffer]()

	obj := p.Get()
	obj.data = append(obj.data, "данные"...)
	p.Put(obj)

	assert.Empty(t, obj.data, "состояние должно сбрасываться до возврата в пул")
	assert.Equal(t, 1, obj.resets)
	assert.NotZero(t, cap(obj.data), "ёмкость сохраняется, ради этого пул и нужен")
}

func TestPutIgnoresNil(t *testing.T) {
	p := pool.New[buffer]()

	require.NotPanics(t, func() { p.Put(nil) })
	assert.NotNil(t, p.Get(), "после nil пул продолжает выдавать объекты")
}

func TestPoolReusesObject(t *testing.T) {
	p := pool.New[buffer]()

	first := p.Get()
	first.data = append(first.data, 'x')
	p.Put(first)

	// sync.Pool не обязан вернуть тот же указатель, но если вернул — объект
	// должен быть чистым
	second := p.Get()
	assert.Empty(t, second.data)
}

func TestPoolWorksWithGeneratedReset(t *testing.T) {
	p := pool.New[models.UserURLItem]()

	item := p.Get()
	item.ShortURL = "http://localhost:8080/abc"
	item.OriginalURL = "https://practicum.yandex.ru"
	p.Put(item)

	assert.Empty(t, item.ShortURL)
	assert.Empty(t, item.OriginalURL)
}

func TestPoolConcurrentUse(t *testing.T) {
	const (
		goroutines = 16
		iterations = 200
	)

	p := pool.New[buffer]()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				obj := p.Get()
				obj.data = append(obj.data, byte(j))
				p.Put(obj)
			}
		}()
	}
	wg.Wait()
}

func BenchmarkPoolGetPut(b *testing.B) {
	p := pool.New[buffer]()

	for b.Loop() {
		obj := p.Get()
		obj.data = append(obj.data, "полезная нагрузка"...)
		p.Put(obj)
	}
}

func BenchmarkWithoutPool(b *testing.B) {
	for b.Loop() {
		obj := &buffer{}
		obj.data = append(obj.data, "полезная нагрузка"...)
		_ = obj
	}
}
