package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noEnv подставляется вместо os.LookupEnv, когда переменные окружения не нужны.
func noEnv(string) (string, bool) {
	return "", false
}

// envMap возвращает функцию поиска переменных окружения по заранее заданной карте.
func envMap(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

// writeConfig сохраняет файл конфигурации во временной директории теста.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestParseDefaults(t *testing.T) {
	cfg, err := parse("shortener", nil, noEnv)
	require.NoError(t, err)

	assert.Equal(t, "localhost:8080", cfg.ServerAddr)
	assert.Equal(t, "http://localhost:8080", cfg.BaseURL)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "/tmp/short-url-db.json", cfg.FileStoragePath)
	assert.Empty(t, cfg.DatabaseDSN)
	assert.False(t, cfg.EnableHTTPS)
}

func TestParseFlags(t *testing.T) {
	args := []string{"-a", ":9090", "-b", "http://short.test", "-f", "/tmp/urls.json", "-d", "postgres://localhost", "-s"}

	cfg, err := parse("shortener", args, noEnv)
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.ServerAddr)
	assert.Equal(t, "http://short.test", cfg.BaseURL)
	assert.Equal(t, "/tmp/urls.json", cfg.FileStoragePath)
	assert.Equal(t, "postgres://localhost", cfg.DatabaseDSN)
	assert.True(t, cfg.EnableHTTPS)
}

func TestParseEnvBeatsFlag(t *testing.T) {
	args := []string{"-a", ":9090", "-b", "http://flag.test"}
	env := envMap(map[string]string{
		"SERVER_ADDRESS": ":7070",
		"BASE_URL":       "http://env.test",
	})

	cfg, err := parse("shortener", args, env)
	require.NoError(t, err)

	assert.Equal(t, ":7070", cfg.ServerAddr)
	assert.Equal(t, "http://env.test", cfg.BaseURL)
}

func TestParseEnableHTTPS(t *testing.T) {
	cfg, err := parse("shortener", nil, envMap(map[string]string{"ENABLE_HTTPS": "true"}))
	require.NoError(t, err)
	assert.True(t, cfg.EnableHTTPS)

	// переменная без значения тоже включает HTTPS
	cfg, err = parse("shortener", nil, envMap(map[string]string{"ENABLE_HTTPS": ""}))
	require.NoError(t, err)
	assert.True(t, cfg.EnableHTTPS)

	// переменная окружения перекрывает флаг и в обратную сторону
	cfg, err = parse("shortener", []string{"-s"}, envMap(map[string]string{"ENABLE_HTTPS": "false"}))
	require.NoError(t, err)
	assert.False(t, cfg.EnableHTTPS)

	_, err = parse("shortener", nil, envMap(map[string]string{"ENABLE_HTTPS": "yes please"}))
	assert.Error(t, err)
}

func TestParseHTTPSSwitchesDefaultBaseURL(t *testing.T) {
	cfg, err := parse("shortener", []string{"-s"}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "https://localhost:8080", cfg.BaseURL,
		"ссылки должны указывать на тот же протокол, по которому отвечает сервис")

	cfg, err = parse("shortener", []string{"-s", "-b", "http://short.test"}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "http://short.test", cfg.BaseURL, "явно заданный адрес не трогаем")

	cfg, err = parse("shortener", nil, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", cfg.BaseURL)
}

func TestParseConfigFile(t *testing.T) {
	path := writeConfig(t, `{
		"server_address": ":8081",
		"base_url": "http://file.test",
		"file_storage_path": "/tmp/file.json",
		"database_dsn": "postgres://file",
		"enable_https": true,
		"log_level": "debug"
	}`)

	cfg, err := parse("shortener", []string{"-c", path}, noEnv)
	require.NoError(t, err)

	assert.Equal(t, ":8081", cfg.ServerAddr)
	assert.Equal(t, "http://file.test", cfg.BaseURL)
	assert.Equal(t, "/tmp/file.json", cfg.FileStoragePath)
	assert.Equal(t, "postgres://file", cfg.DatabaseDSN)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.EnableHTTPS)
}

func TestParseConfigFileKeepsPlainBaseURL(t *testing.T) {
	path := writeConfig(t, `{"base_url": "http://localhost:8080", "enable_https": true}`)

	cfg, err := parse("shortener", []string{"-c", path}, noEnv)
	require.NoError(t, err)

	assert.True(t, cfg.EnableHTTPS)
	assert.Equal(t, "http://localhost:8080", cfg.BaseURL,
		"адрес, заданный в файле, сильнее умолчания и на https не заменяется")
}

func TestParseConfigFileLongFlag(t *testing.T) {
	path := writeConfig(t, `{"server_address": ":8082"}`)

	cfg, err := parse("shortener", []string{"-config", path}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, ":8082", cfg.ServerAddr)
}

func TestParseConfigFileHasLowestPriority(t *testing.T) {
	path := writeConfig(t, `{
		"server_address": ":8081",
		"base_url": "http://file.test",
		"file_storage_path": "/tmp/file.json",
		"enable_https": true
	}`)
	args := []string{"-c", path, "-b", "http://flag.test"}
	env := envMap(map[string]string{
		"SERVER_ADDRESS": ":7070",
		"ENABLE_HTTPS":   "false",
	})

	cfg, err := parse("shortener", args, env)
	require.NoError(t, err)

	assert.Equal(t, ":7070", cfg.ServerAddr, "переменная окружения сильнее файла")
	assert.Equal(t, "http://flag.test", cfg.BaseURL, "флаг сильнее файла")
	assert.False(t, cfg.EnableHTTPS, "переменная окружения сильнее файла и для флага-переключателя")
	assert.Equal(t, "/tmp/file.json", cfg.FileStoragePath, "остальное берётся из файла")
}

func TestParseConfigFileFromEnv(t *testing.T) {
	fromFlag := writeConfig(t, `{"server_address": ":8081"}`)
	fromEnv := writeConfig(t, `{"server_address": ":8082"}`)

	cfg, err := parse("shortener", []string{"-c", fromFlag}, envMap(map[string]string{"CONFIG": fromEnv}))
	require.NoError(t, err)

	assert.Equal(t, ":8082", cfg.ServerAddr, "путь к файлу из окружения сильнее флага")
}

func TestParseConfigFileMissingKeysKeepDefaults(t *testing.T) {
	path := writeConfig(t, `{"base_url": "http://file.test"}`)

	cfg, err := parse("shortener", []string{"-c", path}, noEnv)
	require.NoError(t, err)

	assert.Equal(t, "http://file.test", cfg.BaseURL)
	assert.Equal(t, "localhost:8080", cfg.ServerAddr)
	assert.Equal(t, "/tmp/short-url-db.json", cfg.FileStoragePath)
}

func TestParseConfigFileErrors(t *testing.T) {
	_, err := parse("shortener", []string{"-c", filepath.Join(t.TempDir(), "missing.json")}, noEnv)
	assert.Error(t, err)

	broken := writeConfig(t, `{"server_address": ":8081"`)
	_, err = parse("shortener", []string{"-c", broken}, noEnv)
	assert.Error(t, err)
}

func TestParseUnknownFlag(t *testing.T) {
	_, err := parse("shortener", []string{"-unknown"}, noEnv)
	assert.Error(t, err)
}

func TestParseTrustedSubnet(t *testing.T) {
	cfg, err := parse("shortener", nil, noEnv)
	require.NoError(t, err)
	assert.Empty(t, cfg.TrustedSubnet, "по умолчанию доверенной подсети нет")

	cfg, err = parse("shortener", []string{"-t", "192.168.1.0/24"}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.0/24", cfg.TrustedSubnet)

	cfg, err = parse("shortener", []string{"-t", "192.168.1.0/24"},
		envMap(map[string]string{"TRUSTED_SUBNET": "10.0.0.0/8"}))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.0/8", cfg.TrustedSubnet, "переменная окружения сильнее флага")
}

func TestParseTrustedSubnetFromFile(t *testing.T) {
	path := writeConfig(t, `{"trusted_subnet": "172.16.0.0/12"}`)

	cfg, err := parse("shortener", []string{"-c", path}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "172.16.0.0/12", cfg.TrustedSubnet)

	cfg, err = parse("shortener", []string{"-c", path, "-t", "192.168.1.0/24"}, noEnv)
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.0/24", cfg.TrustedSubnet, "флаг сильнее файла")
}
