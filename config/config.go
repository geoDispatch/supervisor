package config

import (
	"os"
	"strconv"
)

type Config struct {
	ServerPort              string

	NokiaNacBaseURL         string
	MockNokiaNacBaseURL     string
	NokiaNacAPIKey          string
	NokiaNacHost            string
	NokiaNacToken           string

	CAMARALocationMaxAgeSec int
	CamaraConcurrency       int
	
	AgentURL                string
	DatabaseURL             string

	SupervisorPublicSubnet string
	QoSProfileInitial      string
	QoSProfileUpgrade      string
	QoSSessionDuration     int
	QoSExtendDuration      int

	CongestionWebhookURL   string
	CongestionWebhookToken string
}

func Load() *Config {
	return &Config{
		ServerPort:              getEnv("SERVER_PORT", "8080"),
		
		NokiaNacBaseURL:     	 getEnv("NOKIA_NAC_BASE_URL", "https://network-as-code.nokia.rapidapi.com"),
		MockNokiaNacBaseURL: 	 getEnv("MOCK_NOKIA_NAC_BASE_URL", "http://localhost:8081"),
		NokiaNacAPIKey:          getEnv("NOKIA_NAC_API_KEY", ""),
		NokiaNacHost:            getEnv("NOKIA_NAC_HOST", "network-as-code.nokia.rapidapi.com"),
		NokiaNacToken:           getEnv("NOKIA_NAC_TOKEN", ""),
		
		CAMARALocationMaxAgeSec: intEnv("CAMARA_LOCATION_MAX_AGE_SEC", 600),
		CamaraConcurrency:       intEnv("CAMARA_CONCURRENCY", 50),
		
		AgentURL:                getEnv("AGENT_URL", "http://localhost:5000/decide"),
		DatabaseURL:             getEnv("DATABASE_URL", "mock"),

		SupervisorPublicSubnet: getEnv("SUPERVISOR_PUBLIC_SUBNET", "203.0.113.0/24"),
		QoSProfileInitial:      getEnv("QOS_PROFILE_INITIAL", "QOS_M"),
		QoSProfileUpgrade:      getEnv("QOS_PROFILE_UPGRADE", "QOS_L"),
		QoSSessionDuration:     intEnv("QOS_SESSION_DURATION_SEC", 7200),
		QoSExtendDuration:      intEnv("QOS_EXTEND_DURATION_SEC", 3600),

		CongestionWebhookURL:   os.Getenv("CONGESTION_WEBHOOK_URL"),
		CongestionWebhookToken: os.Getenv("CONGESTION_WEBHOOK_TOKEN"),
	}
}

func (c *Config) IsReal() bool {
	return c.NokiaNacAPIKey != ""
}

func (c *Config) BaseURL() string {
	if c.IsReal() {
		return c.NokiaNacBaseURL
	}
	return c.MockNokiaNacBaseURL
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}