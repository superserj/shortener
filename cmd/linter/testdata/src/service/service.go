package service

import (
	"errors"
	"log"
	"os"
)

var errStopped = errors.New("сервис остановлен")

// Start завершает процесс вместо возврата ошибки — так делать нельзя.
func Start() {
	log.Fatal("не удалось стартовать") // want "вызов log\\.Fatal вне функции main пакета main"
}

// Stop обрывает процесс из библиотечного кода.
func Stop() {
	os.Exit(1) // want "вызов os\\.Exit вне функции main пакета main"
}

// Check паникует вместо возврата ошибки.
func Check(ok bool) {
	if !ok {
		panic(errStopped) // want "использование встроенной функции panic"
	}
}

// Reload вызывает выход из вложенного литерала.
func Reload() {
	func() {
		os.Exit(2) // want "вызов os\\.Exit вне функции main пакета main"
	}()
}

// Parenthesized прячет вызовы за скобками — от этого они не перестают
// завершать процесс.
func Parenthesized() {
	(os.Exit)(3)      // want "вызов os\\.Exit вне функции main пакета main"
	(panic)("сбой")   // want "использование встроенной функции panic"
	(log.Fatal)("не") // want "вызов log\\.Fatal вне функции main пакета main"
}

// Report — корректный вариант: ошибка возвращается вызывающей стороне.
func Report() error {
	logger := log.Default()
	logger.Print("перезапуск")
	return errStopped
}
