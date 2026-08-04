// Пакет config собирает настройки сервиса из флагов и переменных окружения.
// Переменная окружения имеет приоритет над флагом.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Config — настройки сервиса.
type Config struct {
	ServerAddr      string
	BaseURL         string
	LogLevel        string
	FileStoragePath string
	DatabaseDSN     string
	AuthSecret      string
	AuditFile       string
	AuditURL        string
	EnableHTTPS     bool
}

// New разбирает аргументы командной строки и переменные окружения и возвращает
// настройки сервиса.
func New() (*Config, error) {
	return parse(os.Args[0], os.Args[1:], os.LookupEnv)
}

// parse вынесен из New, чтобы тесты подавали свои аргументы и переменные
// окружения, не трогая глобальное состояние процесса.
func parse(name string, args []string, lookupEnv func(string) (string, bool)) (*Config, error) {
	cfg := &Config{}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.ServerAddr, "a", "localhost:8080", "address to run HTTP server")
	fs.StringVar(&cfg.BaseURL, "b", "http://localhost:8080", "base address for shortened URL")
	fs.StringVar(&cfg.LogLevel, "l", "info", "log level")
	fs.StringVar(&cfg.FileStoragePath, "f", "/tmp/short-url-db.json", "path to file storage")
	fs.StringVar(&cfg.DatabaseDSN, "d", "", "postgres DSN")
	fs.BoolVar(&cfg.EnableHTTPS, "s", false, "serve HTTPS with a self-signed certificate")
	fs.StringVar(&cfg.AuthSecret, "auth-secret", "shortener-default-secret", "secret key for auth cookie signature")
	fs.StringVar(&cfg.AuditFile, "audit-file", "", "path to audit log file, empty disables file audit")
	fs.StringVar(&cfg.AuditURL, "audit-url", "", "url of remote audit sink, empty disables remote audit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	envString := func(key string, dst *string) {
		if v, ok := lookupEnv(key); ok {
			*dst = v
		}
	}

	envString("SERVER_ADDRESS", &cfg.ServerAddr)
	envString("BASE_URL", &cfg.BaseURL)
	envString("LOG_LEVEL", &cfg.LogLevel)
	envString("FILE_STORAGE_PATH", &cfg.FileStoragePath)
	envString("DATABASE_DSN", &cfg.DatabaseDSN)
	envString("AUTH_SECRET", &cfg.AuthSecret)
	envString("AUDIT_FILE", &cfg.AuditFile)
	envString("AUDIT_URL", &cfg.AuditURL)

	if v, ok := lookupEnv("ENABLE_HTTPS"); ok {
		enabled, err := parseBool(v)
		if err != nil {
			return nil, fmt.Errorf("parse ENABLE_HTTPS: %w", err)
		}
		cfg.EnableHTTPS = enabled
	}

	return cfg, nil
}

// parseBool разбирает значение ENABLE_HTTPS. Переменная без значения включает
// HTTPS: так удобнее задавать её в окружении контейнера.
func parseBool(v string) (bool, error) {
	if v == "" {
		return true, nil
	}
	return strconv.ParseBool(v)
}
