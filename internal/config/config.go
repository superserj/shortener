// Пакет config собирает настройки сервиса из флагов и переменных окружения.
// Переменная окружения имеет приоритет над флагом.
package config

import (
	"flag"
	"os"
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
}

// New разбирает флаги и переменные окружения и возвращает настройки.
func New() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.ServerAddr, "a", "localhost:8080", "address to run HTTP server")
	flag.StringVar(&cfg.BaseURL, "b", "http://localhost:8080", "base address for shortened URL")
	flag.StringVar(&cfg.LogLevel, "l", "info", "log level")
	flag.StringVar(&cfg.FileStoragePath, "f", "/tmp/short-url-db.json", "path to file storage")
	flag.StringVar(&cfg.DatabaseDSN, "d", "", "postgres DSN")
	flag.StringVar(&cfg.AuthSecret, "s", "shortener-default-secret", "secret key for auth cookie signature")
	flag.StringVar(&cfg.AuditFile, "audit-file", "", "path to audit log file, empty disables file audit")
	flag.StringVar(&cfg.AuditURL, "audit-url", "", "url of remote audit sink, empty disables remote audit")

	flag.Parse()

	if v, ok := os.LookupEnv("SERVER_ADDRESS"); ok {
		cfg.ServerAddr = v
	}
	if v, ok := os.LookupEnv("BASE_URL"); ok {
		cfg.BaseURL = v
	}
	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("FILE_STORAGE_PATH"); ok {
		cfg.FileStoragePath = v
	}
	if v, ok := os.LookupEnv("DATABASE_DSN"); ok {
		cfg.DatabaseDSN = v
	}
	if v, ok := os.LookupEnv("AUTH_SECRET"); ok {
		cfg.AuthSecret = v
	}
	if v, ok := os.LookupEnv("AUDIT_FILE"); ok {
		cfg.AuditFile = v
	}
	if v, ok := os.LookupEnv("AUDIT_URL"); ok {
		cfg.AuditURL = v
	}

	return cfg
}
