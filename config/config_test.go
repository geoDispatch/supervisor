package config

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// unsetEnv removes key for the duration of the test (t.Setenv restores it).
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

var newKeys = []string{
	"GEODISPATCH_ENV", "ALLOWED_ORIGINS", "AGENT_TIMEOUT_SEC", "AGENT_BATCH_SIZE",
	"CAMARA_TIMEOUT_MS", "CAMARA_REACHABILITY_TIMEOUT_MS", "CAMARA_CONCURRENCY",
	"PIPELINE_TIMEOUT_SEC", "SENSOR_MAX_BODY_BYTES", "WS_CLIENT_QUEUE_LIMIT",
	"WS_WRITE_TIMEOUT_SEC", "WS_HEARTBEAT_SEC", "SMS_GATEWAY",
	"AGENT_URL", "NOKIA_NAC_API_KEY", "SERVER_PORT", "DATABASE_URL",
}

func TestDefaults(t *testing.T) {
	unsetEnv(t, newKeys...)
	c := Load()

	checks := []struct {
		name      string
		got, want any
	}{
		{"Env", c.Env, EnvDevelopment},
		{"AllowedOrigins", len(c.AllowedOrigins), 0},
		{"AgentTimeout", c.AgentTimeout, 120 * time.Second},
		{"AgentBatchSize", c.AgentBatchSize, 20},
		{"CamaraTimeout", c.CamaraTimeout, 5000 * time.Millisecond},
		{"CamaraReachabilityTimeout", c.CamaraReachabilityTimeout, 1000 * time.Millisecond},
		{"CamaraConcurrency", c.CamaraConcurrency, 50},
		{"PipelineTimeout", c.PipelineTimeout, 900 * time.Second},
		{"SensorMaxBodyBytes", c.SensorMaxBodyBytes, int64(16384)},
		{"WSClientQueueLimit", c.WSClientQueueLimit, 65536},
		{"WSWriteTimeout", c.WSWriteTimeout, 10 * time.Second},
		{"WSHeartbeat", c.WSHeartbeat, 15 * time.Second},
		{"SMSGateway", c.SMSGateway, ""},
		{"SMSConfigured", c.SMSConfigured(), false},
		{"ServerPort", c.ServerPort, "8080"},
		{"AgentURL", c.AgentURL, "http://localhost:8082/decide"},
		{"DatabaseURL", c.DatabaseURL, "mock"},
		{"NetworkSource", c.NetworkSource(), "mock_camara"},
		{"AgentHealthURL", c.AgentHealthURL(), "http://localhost:8082/health"},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}
}

func TestOverrides(t *testing.T) {
	unsetEnv(t, newKeys...)
	t.Setenv("GEODISPATCH_ENV", " Production ")
	t.Setenv("ALLOWED_ORIGINS", " https://ops.example.org, ,http://localhost:5173 ,")
	t.Setenv("AGENT_TIMEOUT_SEC", "30")
	t.Setenv("AGENT_BATCH_SIZE", "5")
	t.Setenv("CAMARA_TIMEOUT_MS", "250")
	t.Setenv("CAMARA_REACHABILITY_TIMEOUT_MS", "100")
	t.Setenv("CAMARA_CONCURRENCY", "8")
	t.Setenv("PIPELINE_TIMEOUT_SEC", "60")
	t.Setenv("SENSOR_MAX_BODY_BYTES", "1024")
	t.Setenv("WS_CLIENT_QUEUE_LIMIT", "100")
	t.Setenv("WS_WRITE_TIMEOUT_SEC", "3")
	t.Setenv("WS_HEARTBEAT_SEC", "2")
	t.Setenv("SMS_GATEWAY", " twilio ")
	t.Setenv("NOKIA_NAC_API_KEY", "secret")

	c := Load()
	if c.Env != EnvProduction {
		t.Errorf("Env = %q", c.Env)
	}
	if want := []string{"https://ops.example.org", "http://localhost:5173"}; !reflect.DeepEqual(c.AllowedOrigins, want) {
		t.Errorf("AllowedOrigins = %q, want %q", c.AllowedOrigins, want)
	}
	if c.AgentTimeout != 30*time.Second || c.AgentBatchSize != 5 ||
		c.CamaraTimeout != 250*time.Millisecond || c.CamaraReachabilityTimeout != 100*time.Millisecond ||
		c.CamaraConcurrency != 8 || c.PipelineTimeout != 60*time.Second ||
		c.SensorMaxBodyBytes != 1024 || c.WSClientQueueLimit != 100 ||
		c.WSWriteTimeout != 3*time.Second || c.WSHeartbeat != 2*time.Second {
		t.Errorf("overrides not applied: %+v", c)
	}
	if c.SMSGateway != "twilio" || !c.SMSConfigured() {
		t.Errorf("SMSGateway = %q", c.SMSGateway)
	}
	if c.NetworkSource() != "nokia_nac" {
		t.Errorf("NetworkSource = %q with an API key", c.NetworkSource())
	}
}

func TestEnvName(t *testing.T) {
	cases := map[string]string{
		"":             EnvDevelopment,
		"development":  EnvDevelopment,
		"DEVELOPMENT":  EnvDevelopment,
		"production":   EnvProduction,
		"prod":         EnvProduction, // unknown fails closed
		"staging":      EnvProduction,
		" production ": EnvProduction,
	}
	for in, want := range cases {
		t.Setenv("GEODISPATCH_ENV", in)
		if got := Load().Env; got != want {
			t.Errorf("GEODISPATCH_ENV=%q → %q, want %q", in, got, want)
		}
	}
}

func TestAgentBatchSizeClamp(t *testing.T) {
	cases := map[string]int{
		"":    20,
		"abc": 20,
		"1":   1,
		"7":   7,
		"20":  20,
		"21":  20,
		"500": 20,
		"0":   1,
		"-3":  1,
	}
	for in, want := range cases {
		t.Setenv("AGENT_BATCH_SIZE", in)
		if got := Load().AgentBatchSize; got != want {
			t.Errorf("AGENT_BATCH_SIZE=%q → %d, want %d", in, got, want)
		}
	}
}

func TestInvalidNumbersFallBack(t *testing.T) {
	for _, v := range []string{"0", "-5", "ten", "1.5"} {
		t.Setenv("PIPELINE_TIMEOUT_SEC", v)
		t.Setenv("SENSOR_MAX_BODY_BYTES", v)
		t.Setenv("WS_HEARTBEAT_SEC", v)
		c := Load()
		if c.PipelineTimeout != 900*time.Second || c.SensorMaxBodyBytes != 16384 || c.WSHeartbeat != 15*time.Second {
			t.Errorf("value %q should fall back to defaults: %v %v %v", v, c.PipelineTimeout, c.SensorMaxBodyBytes, c.WSHeartbeat)
		}
	}
}

func TestAgentHealthURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:5000/decide":         "http://localhost:5000/health",
		"http://agent:5000/decide/":            "http://agent:5000/health",
		"http://agent:5000":                    "http://agent:5000/health",
		"http://agent:5000/":                   "http://agent:5000/health",
		"https://ai.example.org/api/v1/decide": "https://ai.example.org/api/v1/health",
		"http://agent:5000/decide?k=1":         "http://agent:5000/health?k=1",
		"agent:5000/decide":                    "",
		"":                                     "",
	}
	for in, want := range cases {
		c := &Config{AgentURL: in}
		if got := c.AgentHealthURL(); got != want {
			t.Errorf("AgentHealthURL(%q) = %q, want %q", in, got, want)
		}
	}
}
