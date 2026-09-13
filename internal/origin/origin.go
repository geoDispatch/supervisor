// Package origin decides which browser origins may talk to the supervisor
// (WebSocket upgrades and the CORS-enabled HTTP routes).
package origin

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Environments. Anything that is not "development" is treated as production
// so a typo fails closed (fewer origins), never open.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// DevelopmentDefaults are the origins allowed in development when
// ALLOWED_ORIGINS is empty: the Vite dev server, vite preview and a
// conventional :3000, each on localhost and 127.0.0.1. Production has none.
var DevelopmentDefaults = []string{
	"http://localhost:5173", "http://127.0.0.1:5173",
	"http://localhost:4173", "http://127.0.0.1:4173",
	"http://localhost:3000", "http://127.0.0.1:3000",
}

// Policy is an immutable origin allowlist. The zero Policy allows only
// requests without an Origin header and same-origin requests.
type Policy struct {
	env      string
	allowAll bool                // "*" in development only
	allowed  map[string]struct{} // canonical "scheme://host:port"
	origins  []string            // canonical, in configuration order, for logging
}

// NewPolicy builds the policy for env from the configured origins. An empty
// list means the defaults of env. "*" is honoured only in development; in
// production it is ignored. Malformed entries are skipped. Every such
// decision is logged as a warning.
func NewPolicy(env string, allowed []string) Policy {
	p := Policy{env: EnvProduction, allowed: map[string]struct{}{}}
	if strings.EqualFold(strings.TrimSpace(env), EnvDevelopment) {
		p.env = EnvDevelopment
	}

	var entries []string
	for _, a := range allowed {
		if a = strings.TrimSpace(a); a != "" {
			entries = append(entries, a)
		}
	}
	if len(entries) == 0 && p.env == EnvDevelopment {
		entries = DevelopmentDefaults
	}

	for _, e := range entries {
		if e == "*" {
			if p.env == EnvDevelopment {
				log.Printf("[origin] WARNING: ALLOWED_ORIGINS contains \"*\": every browser origin is allowed (development only)")
				p.allowAll = true
			} else {
				log.Printf("[origin] WARNING: ignoring \"*\" in ALLOWED_ORIGINS: wildcards are not honoured in production")
			}
			continue
		}
		c, ok := canonical(e)
		if !ok {
			log.Printf("[origin] WARNING: ignoring malformed ALLOWED_ORIGINS entry %q (want scheme://host[:port], scheme http or https)", e)
			continue
		}
		if _, dup := p.allowed[c.origin]; !dup {
			p.allowed[c.origin] = struct{}{}
			p.origins = append(p.origins, c.origin)
		}
	}
	return p
}

// Env returns "development" or "production".
func (p Policy) Env() string {
	if p.env == "" {
		return EnvProduction
	}
	return p.env
}

// AllowsAll reports whether "*" is in effect (development only).
func (p Policy) AllowsAll() bool { return p.allowAll }

// Origins returns the canonical allowlist (a copy), for startup logging.
func (p Policy) Origins() []string { return append([]string(nil), p.origins...) }

// Allowed reports whether r may be served: it has no Origin header (a
// non-browser client), or its Origin is on the allowlist, or the Origin's
// host:port equals the request Host (same origin). Scheme and host compare
// case-insensitively and default ports (80/443) are normalised.
func (p Policy) Allowed(r *http.Request) bool {
	raw := r.Header.Get("Origin")
	if raw == "" {
		return true
	}
	if p.allowAll {
		return true
	}
	o, ok := canonical(raw)
	if !ok {
		return false // e.g. "null" from sandboxed frames and file:// pages
	}
	if _, ok := p.allowed[o.origin]; ok {
		return true
	}
	return sameHost(o, r.Host)
}

// CORS wraps the non-WebSocket routes (/sensor, /health, /capabilities).
// Every OPTIONS request is answered here and never reaches next: 204 with the
// CORS headers when the origin is allowed, 403 when it is not. Other requests
// from an allowed origin get Access-Control-Allow-Origin; requests from a
// disallowed origin pass through without it (the browser then hides the
// response, and /sensor rejects them itself with 403).
func (p Policy) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// The response depends on Origin whatever the verdict, so shared
		// caches must key on it.
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := p.Allowed(r)

		if r.Method == http.MethodOptions {
			if !allowed {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
				return
			}
			if origin != "" {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if origin != "" && allowed {
			// Echo the header byte for byte: browsers compare it exactly.
			h.Set("Access-Control-Allow-Origin", origin)
		}
		next.ServeHTTP(w, r)
	})
}

// parsed is a normalised http(s) origin.
type parsed struct {
	origin string // "scheme://host:port", lower case, explicit port
	scheme string // "http" or "https"
	host   string // lower case, without IPv6 brackets
	port   string // explicit or the scheme default
}

// canonical parses "scheme://host[:port]" (a trailing "/" is tolerated).
// Paths, queries, fragments, user info and non-http(s) schemes are rejected.
func canonical(s string) (parsed, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return parsed{}, false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return parsed{}, false
	}
	if scheme != "http" && scheme != "https" {
		return parsed{}, false
	}
	port := u.Port()
	if port == "" {
		port = defaultPort(scheme)
	}
	return parsed{origin: scheme + "://" + net.JoinHostPort(host, port), scheme: scheme, host: host, port: port}, true
}

// sameHost reports whether the request Host names the same host:port as the
// origin. A Host without a port takes the default port of the origin's scheme
// (the supervisor may sit behind a TLS proxy, so it cannot know its own).
func sameHost(o parsed, requestHost string) bool {
	if requestHost == "" {
		return false
	}
	u := &url.URL{Host: requestHost}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		port = defaultPort(o.scheme)
	}
	return host == o.host && port == o.port
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
