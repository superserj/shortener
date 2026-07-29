package main

import (
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("не передан путь к конфигурации")
	}

	go func() {
		os.Exit(1) // want "вызов os\\.Exit вне функции main пакета main"
	}()

	if err := run(); err != nil {
		log.Fatalf("запуск: %v", err)
	}

	os.Exit(0)
}

func run() error {
	defer os.Exit(1)               // want "вызов os\\.Exit вне функции main пакета main"
	log.Fatalln("шаг")             // want "вызов log\\.Fatalln вне функции main пакета main"
	panic("сюда управление дойти") // want "использование встроенной функции panic"
}
