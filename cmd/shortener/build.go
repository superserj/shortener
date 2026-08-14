package main

import (
	"fmt"
	"io"
)

// unknownBuildValue подставляется вместо значения, не заданного при сборке.
const unknownBuildValue = "N/A"

// Сведения о сборке подставляются компоновщиком, поэтому в исходном коде
// переменные пустые:
//
//	go build -ldflags "\
//	  -X main.buildVersion=v1.0.1 \
//	  -X 'main.buildDate=$(date +'%Y/%m/%d %H:%M:%S')' \
//	  -X main.buildCommit=$(git rev-parse --short HEAD)" \
//	  -o shortener ./cmd/shortener
//
// Значения обязательно строковые: -X умеет задавать только строки.
var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

// printBuildInfo печатает сведения о сборке. Вывод идёт в stdout до старта
// логгера, чтобы информация не зависела от настроек логирования.
func printBuildInfo(w io.Writer) {
	fmt.Fprintf(w, "Build version: %s\n", buildValue(buildVersion))
	fmt.Fprintf(w, "Build date: %s\n", buildValue(buildDate))
	fmt.Fprintf(w, "Build commit: %s\n", buildValue(buildCommit))
}

// buildValue заменяет незаданное значение на заглушку.
func buildValue(value string) string {
	if value == "" {
		return unknownBuildValue
	}
	return value
}
