package config

import (
	"log"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// Environments accepted in GEODISPATCH_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// MaxAgentBatchSize is the upper bound of AGENT_BATCH_SIZE: the agent
// contract allows at most 20 devices per request.
const MaxAgentBatchSize = 20

type Config struct {
	ServerPort string

	// Env is "development" or "production" (GEODISPATCH_ENV). It selects the
	// default origin allowlist and whether "*" is honoured.
	Env string
	// AllowedOrigins is ALLOWED_ORIGINS split on commas, blanks dropped. Empty
	// means "use the defaults of Env" — origin.NewPolicy applies them.
	AllowedOrigins []string

	NokiaNacBaseURL     string
	MockNokiaNacBaseURL string
	NokiaNacAPIKey      string
	NokiaNacHost        string
	NokiaNacToken       string

	CAMARALocationMaxAgeSec   int
	CamaraConcurrency         int           // triage fan-out
	CamaraTimeout             time.Duration // per CAMARA HTTP call
	CamaraReachabilityTimeout time.Duration // reachability lookup (shorter: a miss only means NOT_CONNECTED is assumed)

	AgentURL       string
	AgentTimeout   time.Duration // per-batch agent HTTP timeout
	AgentBatchSize int           // devices per AI request, 1..MaxAgentBatchSize
	DatabaseURL    string

	PipelineTimeout time.Duration // whole-pipeline deadline

	SensorMaxBodyBytes int64 // POST /sensor body limit

	WSClientQueueLimit int           // frames queued per client before it is dropped as slow
	WSWriteTimeout     time.Duration // per-frame write deadline
	WSHeartbeat        time.Duration // heartbeat period

	// SMSGateway is empty today: SMS is not configured and nothing is ever
	// reported as sent.
	SMSGateway string

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
		ServerPort: getEnv("SERVER_PORT", "8080"),

		Env:            envName(os.Getenv("GEODISPATCH_ENV")),
		AllowedOrigins: listEnv("ALLOWED_ORIGINS"),

		NokiaNacBaseURL:     getEnv("NOKIA_NAC_BASE_URL", "https://network-as-code.nokia.rapidapi.com"),
		MockNokiaNacBaseURL: getEnv("MOCK_NOKIA_NAC_BASE_URL", "http://localhost:8081"),
		NokiaNacAPIKey:      getEnv("NOKIA_NAC_API_KEY", ""),
		NokiaNacHost:        getEnv("NOKIA_NAC_HOST", "network-as-code.nokia.rapidapi.com"),
		NokiaNacToken:       getEnv("NOKIA_NAC_TOKEN", ""),

		CAMARALocationMaxAgeSec:   intEnv("CAMARA_LOCATION_MAX_AGE_SEC", 600),
		CamaraConcurrency:         intEnv("CAMARA_CONCURRENCY", 50),
		CamaraTimeout:             durationEnv("CAMARA_TIMEOUT_MS", 5000, time.Millisecond),
		CamaraReachabilityTimeout: durationEnv("CAMARA_REACHABILITY_TIMEOUT_MS", 1000, time.Millisecond),

		// The mock agent's port. Not 5000: macOS gives that to AirPlay.
		AgentURL:       getEnv("AGENT_URL", "http://localhost:8082/decide"),
		AgentTimeout:   durationEnv("AGENT_TIMEOUT_SEC", 120, time.Second),
		AgentBatchSize: batchSizeEnv("AGENT_BATCH_SIZE"),
		DatabaseURL:    getEnv("DATABASE_URL", "mock"),

		PipelineTimeout: durationEnv("PIPELINE_TIMEOUT_SEC", 900, time.Second),

		SensorMaxBodyBytes: int64(intEnv("SENSOR_MAX_BODY_BYTES", 16384)),

		WSClientQueueLimit: intEnv("WS_CLIENT_QUEUE_LIMIT", 65536),
		WSWriteTimeout:     durationEnv("WS_WRITE_TIMEOUT_SEC", 10, time.Second),
		WSHeartbeat:        durationEnv("WS_HEARTBEAT_SEC", 15, time.Second),

		SMSGateway: strings.TrimSpace(os.Getenv("SMS_GATEWAY")),

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

// NetworkSource is the CAMARA source this configuration DECLARES:
// "nokia_nac" when a Nokia NaC API key is set, else "mock_camara". Nothing
// verifies it, so the dashboard labels it as unverified.
func (c *Config) NetworkSource() string {
	if c.IsReal() {
		return models.NetworkSourceNokiaNAC
	}
	return models.NetworkSourceMockCAMARA
}

// AgentHealthURL is AGENT_URL with its last path segment replaced by
// "health" (".../decide" → ".../health"; no path → "/health"). Only the path
// changes. It returns "" when AGENT_URL is not an absolute URL.
func (c *Config) AgentHealthURL() string {
	u, err := url.Parse(c.AgentURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	dir := path.Dir(strings.TrimSuffix(u.Path, "/"))
	if dir == "." {
		dir = "/"
	}
	u.Path = path.Join(dir, "health")
	u.RawPath = ""
	return u.String()
}

// SMSConfigured reports whether an SMS gateway is configured.
func (c *Config) SMSConfigured() bool {
	return c.SMSGateway != ""
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

// intEnv returns the positive integer in key, or fallback when it is unset,
// malformed or ≤ 0.
func intEnv(key string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return fallback
}

// durationEnv reads a positive integer count of unit (e.g. seconds).
func durationEnv(key string, fallback int, unit time.Duration) time.Duration {
	return time.Duration(intEnv(key, fallback)) * unit
}

// envName normalises GEODISPATCH_ENV. Unset or empty means development;
// an unknown value is treated as production (fewer origins, no "*") so a
// typo fails closed, and is logged.
func envName(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", EnvDevelopment:
		return EnvDevelopment
	case EnvProduction:
		return EnvProduction
	}
	log.Printf("[config] WARNING: GEODISPATCH_ENV=%q is not development|production; using production", v)
	return EnvProduction
}

// listEnv splits a comma-separated variable, trimming entries and dropping
// blanks. Unset or empty returns nil.
func listEnv(key string) []string {
	var out []string
	for _, s := range strings.Split(os.Getenv(key), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// batchSizeEnv reads AGENT_BATCH_SIZE: unset or malformed → the maximum;
// out of range → clamped to 1..MaxAgentBatchSize (logged).
func batchSizeEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	n, err := strconv.Atoi(raw)
	if err != nil {
		if raw != "" {
			log.Printf("[config] WARNING: %s=%q is not an integer; using %d", key, raw, MaxAgentBatchSize)
		}
		return MaxAgentBatchSize
	}
	clamped := min(max(n, 1), MaxAgentBatchSize)
	if clamped != n {
		log.Printf("[config] WARNING: %s=%d is outside 1..%d; using %d", key, n, MaxAgentBatchSize, clamped)
	}
	return clamped
}
