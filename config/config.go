package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	ServerPort string

	// Nokia NaC
	NokiaNaCToken   string
	NokiaNaCBaseURL string

	// Python AI Agent
	AgentURL string

	// Database
	DatabaseURL string

	// SMS Gateway
	AfricasTalkingAPIKey    string
	AfricasTalkingUsername  string

	// Ollama
	OllamaURL string

	// Semaphore limits
	CAMARAConcurrency int
	SMSConcurrency    int
}

func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("no .env file found, reading from environment")
	}

	return &Config{
		ServerPort:             getEnv("SERVER_PORT", "8080"),
		NokiaNaCToken:          mustEnv("NOKIA_NAC_TOKEN"),
		NokiaNaCBaseURL:        getEnv("NOKIA_NAC_BASE_URL", "https://network-as-code.nokia.com/api"),
		AgentURL:               getEnv("AGENT_URL", "http://localhost:5000/decide"),
		DatabaseURL:            mustEnv("DATABASE_URL"),
		AfricasTalkingAPIKey:   mustEnv("AFRICASTALKING_API_KEY"),
		AfricasTalkingUsername: mustEnv("AFRICASTALKING_USERNAME"),
		OllamaURL:              getEnv("OLLAMA_URL", "http://localhost:11434"),
		CAMARAConcurrency:      getEnvInt("CAMARA_CONCURRENCY", 50),
		SMSConcurrency:         getEnvInt("SMS_CONCURRENCY", 100),
	}
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

func mustEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("missing required environment variable: %s", key)
	}
	return val
}

func getEnvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}

	n, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("invalid value for %s, using default %d", key, fallback)
		return fallback
	}
	return n
}