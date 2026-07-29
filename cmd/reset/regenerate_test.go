package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consumerModule зависит от сгенерированного метода: без reset.gen.go пакет не
// разбирается, поэтому повторный запуск проверяет, что генератор не рубит сук,
// на котором сидит.
const consumerModule = `package consumer

// Item сбрасывается сгенерированным методом.
// generate:reset
type Item struct {
	Name string
}

// Clear пользуется методом, которого в исходниках нет: он появляется только
// после генерации.
func Clear(i *Item) {
	i.Reset()
}
`

func TestGenerateIsRepeatable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module consumer\n\ngo 1.26\n"), testFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "consumer.go"), []byte(consumerModule), testFilePerm))

	first, err := generate(dir)
	require.NoError(t, err)
	require.Len(t, first, 1)
	firstSrc, err := os.ReadFile(first[0])
	require.NoError(t, err)

	// второй запуск идёт уже при живом потребителе метода
	second, err := generate(dir)
	require.NoError(t, err)
	require.Equal(t, first, second)

	secondSrc, err := os.ReadFile(second[0])
	require.NoError(t, err)
	assert.Equal(t, string(firstSrc), string(secondSrc), "повторная генерация должна давать тот же файл")
}

func TestGenerateWithRelativeDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module relative\n\ngo 1.26\n"), testFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "relative.go"), []byte(consumerModule), testFilePerm))

	// генератор запускают из корня проекта как go run ./cmd/reset, то есть с
	// путём "."; обход дерева тогда даёт относительные пути, а позиции разбора —
	// абсолютные, и файл не должен принять сам себя за устаревший
	t.Chdir(dir)

	_, err := generate(".")
	require.NoError(t, err)

	_, err = generate(".")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, genFileName))
	require.NoError(t, err, "файл должен пережить повторный запуск")
	assert.Contains(t, string(data), "func (x *Item) Reset() {")
}

func TestGenerateAfterPartialMarkerRemoval(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module partial\n\ngo 1.26\n"), testFilePerm))
	source := filepath.Join(dir, "types.go")
	both := "package partial\n\n// generate:reset\ntype A struct{ N int }\n\n// generate:reset\ntype B struct{ Inner A }\n"
	require.NoError(t, os.WriteFile(source, []byte(both), testFilePerm))

	_, err := generate(dir)
	require.NoError(t, err)

	// маркер снят с A, но B по-прежнему содержит поле этого типа: опираться на
	// метод A.Reset из прошлого файла нельзя, он вот-вот исчезнет
	onlyB := "package partial\n\ntype A struct{ N int }\n\n// generate:reset\ntype B struct{ Inner A }\n"
	require.NoError(t, os.WriteFile(source, []byte(onlyB), testFilePerm))

	written, err := generate(dir)
	require.NoError(t, err)
	require.Len(t, written, 1)

	data, err := os.ReadFile(written[0])
	require.NoError(t, err)
	src := string(data)
	assert.NotContains(t, src, "func (x *A) Reset()")
	assert.NotContains(t, src, "x.Inner.Reset()", "вызов исчезнувшего метода не должен попасть в файл")
	assert.Contains(t, src, "resetZero(&x.Inner)")
}

func TestGenerateKeepsForeignFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module foreign\n\ngo 1.26\n"), testFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foreign.go"),
		[]byte("package foreign\n\n// generate:reset\ntype T struct{ N int }\n"), testFilePerm))

	// имя reset.gen.go занял человек, заголовка генератора в файле нет
	foreign := "package foreign\n\n// Reset написан руками.\nfunc (t *T) Reset() { t.N = -1 }\n"
	path := filepath.Join(dir, genFileName)
	require.NoError(t, os.WriteFile(path, []byte(foreign), testFilePerm))

	_, err := generate(dir)
	require.Error(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, foreign, string(data), "чужой файл должен остаться нетронутым")
}

func TestGenerateKeepsFilesWhenPackageBroken(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module broken\n\ngo 1.26\n"), testFilePerm))
	source := filepath.Join(dir, "broken.go")
	require.NoError(t, os.WriteFile(source, []byte("package broken\n\n// generate:reset\ntype T struct{ N int }\n"), testFilePerm))

	written, err := generate(dir)
	require.NoError(t, err)
	require.Len(t, written, 1)
	before, err := os.ReadFile(written[0])
	require.NoError(t, err)

	// ломаем исходник так, что пакет перестаёт разбираться
	require.NoError(t, os.WriteFile(source, []byte("package broken\n\nfunc Broken() { это не Go }\n"), testFilePerm))

	_, err = generate(dir)
	require.Error(t, err)

	after, err := os.ReadFile(written[0])
	require.NoError(t, err, "сгенерированный файл должен пережить неудачный запуск")
	assert.Equal(t, string(before), string(after))
}
