// Команда linter — статический анализатор исходного кода сервиса.
//
// Анализатор exitcheck сообщает о местах, где программа завершается аварийно,
// минуя отложенные функции: об использовании встроенной функции panic и о
// вызовах log.Fatal и os.Exit вне функции main пакета main.
//
// Сборка и запуск:
//
//	go build -o linter ./cmd/linter
//	./linter ./...
//
// Анализатор собран через singlechecker, поэтому поддерживает стандартные
// флаги: ./linter help exitcheck выводит справку, ./linter -json — отчёт в JSON.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(ExitCheckAnalyzer)
}
