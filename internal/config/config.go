// Пакет config собирает настройки сервиса из файла конфигурации, флагов и
// переменных окружения. Приоритет значений: переменная окружения, затем флаг,
// затем файл конфигурации, затем значение по умолчанию.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
)

const (
	defaultServerAddr = "localhost:8080"
	defaultBaseURL    = "http://localhost:8080"
	// с включённым HTTPS адрес коротких ссылок по умолчанию тоже должен быть
	// https, иначе сервис выдаёт ссылки, по которым сам не отвечает
	defaultBaseURLTLS = "https://localhost:8080"
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
	TrustedSubnet   string
	EnableHTTPS     bool
	ConfigFile      string
}

// fileConfig повторяет настройки в виде JSON. Поля объявлены указателями, чтобы
// отличать отсутствующий ключ от заданного нулевого значения: отсутствующий
// ключ ничего не переопределяет.
type fileConfig struct {
	ServerAddr      *string `json:"server_address"`
	BaseURL         *string `json:"base_url"`
	LogLevel        *string `json:"log_level"`
	FileStoragePath *string `json:"file_storage_path"`
	DatabaseDSN     *string `json:"database_dsn"`
	AuthSecret      *string `json:"auth_secret"`
	AuditFile       *string `json:"audit_file"`
	AuditURL        *string `json:"audit_url"`
	TrustedSubnet   *string `json:"trusted_subnet"`
	EnableHTTPS     *bool   `json:"enable_https"`
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
	fs.StringVar(&cfg.ServerAddr, "a", defaultServerAddr, "address to run HTTP server")
	fs.StringVar(&cfg.BaseURL, "b", defaultBaseURL, "base address for shortened URL")
	fs.StringVar(&cfg.LogLevel, "l", "info", "log level")
	fs.StringVar(&cfg.FileStoragePath, "f", "/tmp/short-url-db.json", "path to file storage")
	fs.StringVar(&cfg.DatabaseDSN, "d", "", "postgres DSN")
	fs.BoolVar(&cfg.EnableHTTPS, "s", false, "serve HTTPS with a self-signed certificate")
	fs.StringVar(&cfg.AuthSecret, "auth-secret", "shortener-default-secret", "secret key for auth cookie signature")
	fs.StringVar(&cfg.AuditFile, "audit-file", "", "path to audit log file, empty disables file audit")
	fs.StringVar(&cfg.AuditURL, "audit-url", "", "url of remote audit sink, empty disables remote audit")
	fs.StringVar(&cfg.TrustedSubnet, "t", "", "CIDR of the subnet allowed to read internal stats, empty forbids everyone")
	fs.StringVar(&cfg.ConfigFile, "c", "", "path to JSON config file")
	fs.StringVar(&cfg.ConfigFile, "config", "", "path to JSON config file, long form of -c")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	// -s стал переключателем, а не строкой с секретом: аргумент после него
	// остановил бы разбор, и следующие флаги молча потерялись бы
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	// set отмечает настройки, заданные флагом или переменной окружения:
	// значения из файла конфигурации их не переопределяют
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	envString := func(key, name string, dst *string) {
		v, ok := lookupEnv(key)
		if !ok {
			return
		}
		*dst = v
		set[name] = true
	}

	envString("SERVER_ADDRESS", "a", &cfg.ServerAddr)
	envString("BASE_URL", "b", &cfg.BaseURL)
	envString("LOG_LEVEL", "l", &cfg.LogLevel)
	envString("FILE_STORAGE_PATH", "f", &cfg.FileStoragePath)
	envString("DATABASE_DSN", "d", &cfg.DatabaseDSN)
	envString("AUTH_SECRET", "auth-secret", &cfg.AuthSecret)
	envString("AUDIT_FILE", "audit-file", &cfg.AuditFile)
	envString("AUDIT_URL", "audit-url", &cfg.AuditURL)
	envString("TRUSTED_SUBNET", "t", &cfg.TrustedSubnet)

	if v, ok := lookupEnv("ENABLE_HTTPS"); ok {
		enabled, err := parseBool(v)
		if err != nil {
			return nil, fmt.Errorf("parse ENABLE_HTTPS: %w", err)
		}
		cfg.EnableHTTPS = enabled
		set["s"] = true
	}
	// пустое значение считаем незаданным, иначе объявленная в окружении, но
	// пустая переменная отменила бы файл, указанный флагом
	if v, ok := lookupEnv("CONFIG"); ok && v != "" {
		cfg.ConfigFile = v
	}

	if cfg.ConfigFile != "" {
		if err := applyFile(cfg, set); err != nil {
			return nil, err
		}
	}

	if cfg.EnableHTTPS && !set["b"] && cfg.BaseURL == defaultBaseURL {
		cfg.BaseURL = defaultBaseURLTLS
	}
	return cfg, nil
}

// applyFile дополняет настройки значениями из файла конфигурации, не трогая те,
// что уже заданы флагом или переменной окружения.
func applyFile(cfg *Config, set map[string]bool) error {
	data, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("parse config file %s: %w", cfg.ConfigFile, err)
	}

	applyString(fc.ServerAddr, "a", set, &cfg.ServerAddr)
	applyString(fc.BaseURL, "b", set, &cfg.BaseURL)
	applyString(fc.LogLevel, "l", set, &cfg.LogLevel)
	applyString(fc.FileStoragePath, "f", set, &cfg.FileStoragePath)
	applyString(fc.DatabaseDSN, "d", set, &cfg.DatabaseDSN)
	applyString(fc.AuthSecret, "auth-secret", set, &cfg.AuthSecret)
	applyString(fc.AuditFile, "audit-file", set, &cfg.AuditFile)
	applyString(fc.AuditURL, "audit-url", set, &cfg.AuditURL)
	applyString(fc.TrustedSubnet, "t", set, &cfg.TrustedSubnet)

	if fc.EnableHTTPS != nil && !set["s"] {
		cfg.EnableHTTPS = *fc.EnableHTTPS
		set["s"] = true
	}
	return nil
}

// applyString переносит значение из файла и отмечает настройку заданной:
// значение из файла сильнее умолчания, и подменять его дальше нельзя.
func applyString(value *string, name string, set map[string]bool, dst *string) {
	if value == nil || set[name] {
		return
	}
	*dst = *value
	set[name] = true
}

// parseBool разбирает значение ENABLE_HTTPS. Переменная без значения включает
// HTTPS: так удобнее задавать её в окружении контейнера.
func parseBool(v string) (bool, error) {
	if v == "" {
		return true, nil
	}
	return strconv.ParseBool(v)
}
