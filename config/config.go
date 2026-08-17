package config

import (
	"os"
	"strconv"
)

type Config struct {
	ServerPort        string
	NokiaNacBaseURL   string
	NokiaNacToken     string
	AgentURL          string
	DatabaseURL       string
	CamaraConcurrency int
}

func Load() *Config {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	concurrencyStr := os.Getenv("CAMARA_CONCURRENCY")
	concurrency := 50
	if concurrencyStr != "" {
		if val, err := strconv.Atoi(concurrencyStr); err == nil {
			concurrency = val
		}
	}

	return &Config{
		ServerPort:        port,
		NokiaNacBaseURL:   getEnv("NOKIA_NAC_BASE_URL", "http://localhost:8081"),
		NokiaNacToken:     getEnv("NOKIA_NAC_TOKEN", "mock_token"),
		AgentURL:          getEnv("AGENT_URL", "http://localhost:5000/decide"),
		DatabaseURL:       getEnv("DATABASE_URL", "mock"),
		CamaraConcurrency: concurrency,
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}