package config

import (
	"flag"
	"os"
)

type Config struct {
	ServerAddr      string
	BaseURL         string
	LogLevel        string
	FileStoragePath string
	DatabaseDSN     string
	AuthSecret      string
}

func New() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.ServerAddr, "a", "localhost:8080", "address to run HTTP server")
	flag.StringVar(&cfg.BaseURL, "b", "http://localhost:8080", "base address for shortened URL")
	flag.StringVar(&cfg.LogLevel, "l", "info", "log level")
	flag.StringVar(&cfg.FileStoragePath, "f", "/tmp/short-url-db.json", "path to file storage")
	flag.StringVar(&cfg.DatabaseDSN, "d", "", "postgres DSN")
	flag.StringVar(&cfg.AuthSecret, "s", "shortener-default-secret", "secret key for auth cookie signature")

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

	return cfg
}
