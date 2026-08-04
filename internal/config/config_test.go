package config

import (
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

func TestParseUnknownFlag(t *testing.T) {
	_, err := parse("shortener", []string{"-unknown"}, noEnv)
	assert.Error(t, err)
}
