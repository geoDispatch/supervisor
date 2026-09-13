package origin

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// captureLog redirects the standard logger for the duration of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(prevFlags) })
	return &buf
}

func request(method, host, origin string) *http.Request {
	r := httptest.NewRequest(method, "http://"+host+"/sensor", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestNewPolicyDefaultsAndWildcard(t *testing.T) {
	cases := []struct {
		name        string
		env         string
		allowed     []string
		wantEnv     string
		wantAll     bool
		wantOrigins []string
		wantLog     string
	}{
		{"development defaults", "development", nil, EnvDevelopment, false, DevelopmentDefaults, ""},
		{"blank entries mean defaults", "Development", []string{" ", ""}, EnvDevelopment, false, DevelopmentDefaults, ""},
		{"production has no defaults", "production", nil, EnvProduction, false, nil, ""},
		{"unknown env fails closed", "prod", nil, EnvProduction, false, nil, ""},
		{"empty env is production at this layer", "", nil, EnvProduction, false, nil, ""},
		{"explicit list replaces defaults", "development", []string{"https://ops.example.org"}, EnvDevelopment, false,
			[]string{"https://ops.example.org:443"}, ""},
		{"wildcard honoured in development", "development", []string{"*"}, EnvDevelopment, true, nil, "every browser origin is allowed"},
		{"wildcard ignored in production", "production", []string{"*", "https://ops.example.org"}, EnvProduction, false,
			[]string{"https://ops.example.org:443"}, "wildcards are not honoured in production"},
		{"malformed entries skipped", "production", []string{"localhost:5173", "http://x.org/path", "ftp://x.org", "HTTP://X.ORG"},
			EnvProduction, false, []string{"http://x.org:80"}, "malformed ALLOWED_ORIGINS entry"},
		{"duplicates collapse", "production", []string{"http://a.org", "http://A.org:80/", "http://a.org:80"}, EnvProduction, false,
			[]string{"http://a.org:80"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLog(t)
			p := NewPolicy(c.env, c.allowed)
			if p.Env() != c.wantEnv {
				t.Errorf("Env() = %q, want %q", p.Env(), c.wantEnv)
			}
			if p.AllowsAll() != c.wantAll {
				t.Errorf("AllowsAll() = %v, want %v", p.AllowsAll(), c.wantAll)
			}
			// DevelopmentDefaults already carry explicit ports, so they are canonical.
			got := p.Origins()
			if len(got) != 0 || len(c.wantOrigins) != 0 {
				if !reflect.DeepEqual(got, c.wantOrigins) {
					t.Errorf("Origins() = %v, want %v", got, c.wantOrigins)
				}
			}
			if c.wantLog != "" && !strings.Contains(logs.String(), c.wantLog) {
				t.Errorf("expected a warning containing %q, log was %q", c.wantLog, logs.String())
			}
			if c.wantLog == "" && logs.Len() != 0 {
				t.Errorf("unexpected log output: %q", logs.String())
			}
		})
	}
}

func TestAllowed(t *testing.T) {
	captureLog(t)
	dev := NewPolicy("development", nil)
	prod := NewPolicy("production", []string{"https://ops.example.org", "http://[::1]:5173"})
	prodWildcard := NewPolicy("production", []string{"*"})
	devWildcard := NewPolicy("development", []string{"*"})

	cases := []struct {
		name   string
		p      Policy
		host   string
		origin string
		want   bool
	}{
		{"no Origin header (curl, sensors)", prod, "supervisor:8080", "", true},
		{"dev default vite", dev, "localhost:8080", "http://localhost:5173", true},
		{"dev default 127.0.0.1 preview", dev, "localhost:8080", "http://127.0.0.1:4173", true},
		{"dev rejects other port", dev, "localhost:8080", "http://localhost:5174", false},
		{"dev rejects https variant of http default", dev, "localhost:8080", "https://localhost:5173", false},
		{"exact allowlist match", prod, "api.example.org", "https://ops.example.org", true},
		{"allowlist case-insensitive scheme and host", prod, "api.example.org", "HTTPS://OPS.Example.ORG", true},
		{"allowlist default port normalised", prod, "api.example.org", "https://ops.example.org:443", true},
		{"allowlist IPv6", prod, "api.example.org", "http://[::1]:5173", true},
		{"not on allowlist", prod, "api.example.org", "https://evil.example.com", false},
		{"suffix trick", prod, "api.example.org", "https://ops.example.org.evil.com", false},
		{"same origin with port", prod, "supervisor.local:8080", "http://supervisor.local:8080", true},
		{"same origin, Host without port, http", prod, "supervisor.local", "http://supervisor.local", true},
		{"same origin, Host :80 vs implicit", prod, "supervisor.local:80", "http://supervisor.local", true},
		{"same origin, https behind proxy", prod, "supervisor.local", "https://supervisor.local", true},
		{"same origin case-insensitive", prod, "Supervisor.Local:8080", "http://SUPERVISOR.local:8080", true},
		{"same host different port", prod, "supervisor.local:8080", "http://supervisor.local:9090", false},
		{"https origin :443 vs Host :80", prod, "supervisor.local:80", "https://supervisor.local", false},
		{"null origin", prod, "supervisor.local", "null", false},
		{"origin with path", prod, "supervisor.local", "http://supervisor.local/x", false},
		{"non-http scheme", prod, "supervisor.local", "file://supervisor.local", false},
		{"production ignores wildcard", prodWildcard, "api.example.org", "https://evil.example.com", false},
		{"development honours wildcard", devWildcard, "localhost:8080", "https://anything.example", true},
		{"zero policy: same origin only", Policy{}, "localhost:8080", "http://localhost:5173", false},
		{"zero policy: same origin ok", Policy{}, "localhost:8080", "http://localhost:8080", true},
	}
	for _, c := range cases {
		if got := c.p.Allowed(request(http.MethodGet, c.host, c.origin)); got != c.want {
			t.Errorf("%s: Allowed(host=%q, origin=%q) = %v, want %v", c.name, c.host, c.origin, got, c.want)
		}
	}
}

func TestCORS(t *testing.T) {
	captureLog(t)
	p := NewPolicy("production", []string{"https://ops.example.org"})
	var reached int
	h := p.CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusTeapot)
	}))

	cases := []struct {
		name        string
		method      string
		origin      string
		wantStatus  int
		wantReached bool
		wantACAO    string
		wantPreflt  bool // Allow-Methods / Allow-Headers / Max-Age present
	}{
		{"preflight allowed", http.MethodOptions, "https://ops.example.org", http.StatusNoContent, false, "https://ops.example.org", true},
		{"preflight disallowed", http.MethodOptions, "https://evil.example.com", http.StatusForbidden, false, "", false},
		{"OPTIONS without Origin", http.MethodOptions, "", http.StatusNoContent, false, "", true},
		{"POST allowed", http.MethodPost, "https://ops.example.org", http.StatusTeapot, true, "https://ops.example.org", false},
		{"POST echoes Origin verbatim", http.MethodPost, "https://OPS.example.org", http.StatusTeapot, true, "https://OPS.example.org", false},
		{"GET disallowed passes through bare", http.MethodGet, "https://evil.example.com", http.StatusTeapot, true, "", false},
		{"GET without Origin", http.MethodGet, "", http.StatusTeapot, true, "", false},
		{"GET same origin", http.MethodGet, "http://supervisor.local:8080", http.StatusTeapot, true, "http://supervisor.local:8080", false},
	}
	for _, c := range cases {
		reached = 0
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request(c.method, "supervisor.local:8080", c.origin))
		res := rec.Result()
		if res.StatusCode != c.wantStatus {
			t.Errorf("%s: status %d, want %d", c.name, res.StatusCode, c.wantStatus)
		}
		if (reached == 1) != c.wantReached {
			t.Errorf("%s: next reached=%v, want %v", c.name, reached == 1, c.wantReached)
		}
		if got := res.Header.Get("Access-Control-Allow-Origin"); got != c.wantACAO {
			t.Errorf("%s: ACAO %q, want %q", c.name, got, c.wantACAO)
		}
		if got := res.Header.Get("Vary"); got != "Origin" {
			t.Errorf("%s: Vary %q, want Origin", c.name, got)
		}
		gotPreflight := res.Header.Get("Access-Control-Allow-Methods") == "GET, POST, OPTIONS" &&
			res.Header.Get("Access-Control-Allow-Headers") == "Content-Type" &&
			res.Header.Get("Access-Control-Max-Age") == "600"
		if gotPreflight != c.wantPreflt {
			t.Errorf("%s: preflight headers present=%v, want %v (%v)", c.name, gotPreflight, c.wantPreflt, res.Header)
		}
		if c.wantStatus == http.StatusForbidden {
			if ct := res.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("%s: 403 content type %q", c.name, ct)
			}
			if body := rec.Body.String(); !strings.Contains(body, `"origin_not_allowed"`) {
				t.Errorf("%s: 403 body %q", c.name, body)
			}
		}
		if !c.wantPreflt && res.Header.Get("Access-Control-Allow-Methods") != "" {
			t.Errorf("%s: non-preflight response carries Allow-Methods", c.name)
		}
	}
}
