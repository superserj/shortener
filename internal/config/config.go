package config

import "flag"

type Config struct {
	ServerAddr string
	BaseURL    string
}

func New() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.ServerAddr, "a", "localhost:8080", "address to run HTTP server")
	flag.StringVar(&cfg.BaseURL, "b", "http://localhost:8080", "base address for shortened URL")

	flag.Parse()

	return cfg
}
