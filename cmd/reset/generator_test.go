package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFilePerm — права на файлы временного модуля, который собирает тест.
const testFilePerm = 0o600

// sampleModule — модуль с одной помеченной структурой на каждый разбираемый
// случай: примитивы, слайс, мапа, указатели, вложенные структуры и типы,
// которые сбрасываются нулевым значением.
const sampleModule = `package sample

// Inner сбрасывается собственным методом, потому что тоже помечен.
// generate:reset
type Inner struct {
	N int
}

// Plain метода Reset не получает: маркера над ним нет.
type Plain struct {
	N int
}

// Manual сбрасывается собственным методом с приёмником-указателем.
type Manual struct {
	N int
}

func (m *Manual) Reset() { m.N = 0 }

// Sample перечисляет все поддерживаемые виды полей.
// generate:reset
type Sample struct {
	I     int
	S     string
	B     bool
	F     float64
	C     complex128
	Sl    []int
	M     map[string]int
	P     *string
	PS    *[]int
	Child *Inner
	Val   Inner
	Other Plain
	Man   Manual
	Ch    chan int
	Fn    func()
	Iface any
	Arr   [3]int
	Res   interface{ Reset() }
	_     int
}

// Box проверяет обобщённые структуры.
// generate:reset
type Box[T any] struct {
	Items []T
	Value T
}
`

func generateSample(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sample\n\ngo 1.26\n"), testFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.go"), []byte(sampleModule), testFilePerm))

	written, err := generate(dir)
	require.NoError(t, err)
	require.Len(t, written, 1)

	data, err := os.ReadFile(written[0])
	require.NoError(t, err)
	return string(data)
}

func TestGenerateResetMethods(t *testing.T) {
	src := generateSample(t)

	t.Run("файл разбирается как корректный Go", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), genFileName, src, parser.AllErrors)
		require.NoError(t, err)
	})

	t.Run("методы только у помеченных структур", func(t *testing.T) {
		assert.Contains(t, src, "func (x *Sample) Reset() {")
		assert.Contains(t, src, "func (x *Inner) Reset() {")
		assert.NotContains(t, src, "func (x *Plain) Reset()")
	})

	t.Run("нулевые значения примитивов", func(t *testing.T) {
		assert.Contains(t, src, "x.I = 0")
		assert.Contains(t, src, `x.S = ""`)
		assert.Contains(t, src, "x.B = false")
		assert.Contains(t, src, "x.F = 0")
		assert.Contains(t, src, "x.C = 0i")
	})

	t.Run("слайс обрезается, мапа очищается", func(t *testing.T) {
		assert.Contains(t, src, "x.Sl = x.Sl[:0]")
		assert.Contains(t, src, "clear(x.M)")
	})

	t.Run("указатели сбрасывают значение под собой", func(t *testing.T) {
		assert.Contains(t, src, "if x.P != nil {")
		assert.Contains(t, src, `(*x.P) = ""`)
		assert.Contains(t, src, "(*x.PS) = (*x.PS)[:0]")
	})

	t.Run("вложенные структуры вызывают свой Reset", func(t *testing.T) {
		assert.Contains(t, src, "if x.Child != nil {")
		assert.Contains(t, src, "x.Child.Reset()")
		assert.Contains(t, src, "x.Val.Reset()")
		// метод объявлен на *Manual, но поле адресуемо, поэтому вызов допустим
		assert.Contains(t, src, "x.Man.Reset()")
	})

	t.Run("остальные типы обнуляются хелпером", func(t *testing.T) {
		assert.Contains(t, src, "resetZero(&x.Other)")
		assert.Contains(t, src, "resetZero(&x.Ch)")
		assert.Contains(t, src, "resetZero(&x.Fn)")
		assert.Contains(t, src, "resetZero(&x.Iface)")
		assert.Contains(t, src, "resetZero(&x.Arr)")
		assert.Contains(t, src, "func resetZero[T any](v *T) {")
	})

	t.Run("интерфейс обнуляется, а не вызывает Reset", func(t *testing.T) {
		// значение интерфейса по умолчанию nil, вызов метода на нём паникует
		assert.Contains(t, src, "resetZero(&x.Res)")
		assert.NotContains(t, src, "x.Res.Reset()")
	})

	t.Run("поле-пустышка пропускается", func(t *testing.T) {
		assert.NotContains(t, src, "x._")
	})

	t.Run("обобщённая структура получает параметры типа в приёмнике", func(t *testing.T) {
		assert.Contains(t, src, "func (x *Box[T]) Reset() {")
		assert.Contains(t, src, "x.Items = x.Items[:0]")
		assert.Contains(t, src, "resetZero(&x.Value)")
	})

	t.Run("заголовок помечает файл сгенерированным", func(t *testing.T) {
		assert.Contains(t, src, genHeader)
		assert.Contains(t, src, "package sample")
	})
}

func TestGenerateRemovesStaleFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stale\n\ngo 1.26\n"), testFilePerm))
	source := filepath.Join(dir, "stale.go")
	require.NoError(t, os.WriteFile(source, []byte("package stale\n\n// generate:reset\ntype T struct{ N int }\n"), testFilePerm))

	written, err := generate(dir)
	require.NoError(t, err)
	require.Len(t, written, 1)

	// маркер сняли — метод не должен пережить следующую генерацию
	require.NoError(t, os.WriteFile(source, []byte("package stale\n\ntype T struct{ N int }\n"), testFilePerm))

	written, err = generate(dir)
	require.NoError(t, err)
	assert.Empty(t, written)
	_, err = os.Stat(filepath.Join(dir, genFileName))
	assert.True(t, os.IsNotExist(err), "устаревший файл должен быть удалён")
}

func TestGenerateRejectsHandwrittenReset(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module manual\n\ngo 1.26\n"), testFilePerm))
	source := "package manual\n\n// generate:reset\ntype T struct{ N int }\n\nfunc (t *T) Reset() { t.N = 0 }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manual.go"), []byte(source), testFilePerm))

	_, err := generate(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "уже объявлен")
}

func TestGenerateSkipsPackagesWithoutMarkers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module empty\n\ngo 1.26\n"), testFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), []byte("package empty\n\ntype T struct{ N int }\n"), testFilePerm))

	written, err := generate(dir)
	require.NoError(t, err)
	assert.Empty(t, written)

	_, err = os.Stat(filepath.Join(dir, genFileName))
	assert.True(t, os.IsNotExist(err), "файл не должен создаваться без помеченных структур")
}
