package dotimport

import (
	. "log"
	. "os"
)

// Halt вызывает os.Exit через точечный импорт, без квалификатора пакета.
func Halt() {
	Exit(1) // want "вызов os\\.Exit вне функции main пакета main"
}

// Die вызывает log.Fatalf через точечный импорт.
func Die(err error) {
	Fatalf("сбой: %v", err) // want "вызов log\\.Fatalf вне функции main пакета main"
}
