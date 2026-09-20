package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter is a per-IP token-bucket limiter. Zero value is not usable;
// build with NewRateLimiter.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*entry
	r       rate.Limit
	b       int
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a limiter that allows rps requests per second per IP,
// with a burst equal to rps (one second of headroom).
func NewRateLimiter(rps int) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*entry),
		r:       rate.Limit(rps),
		b:       rps,
	}
	// Prune stale entries every minute so the map doesn't grow forever.
	go rl.prune()
	return rl
}

func (rl *RateLimiter) get(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e, ok := rl.clients[ip]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(rl.r, rl.b)}
		rl.clients[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (rl *RateLimiter) prune() {
	for range time.Tick(time.Minute) {
		rl.mu.Lock()
		for ip, e := range rl.clients {
			if time.Since(e.lastSeen) > 3*time.Minute {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware wraps next, returning 429 when the per-IP bucket is empty.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.get(ip).Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limit_exceeded"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the real client IP, honouring X-Forwarded-For when set.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take only the first (leftmost) address — the client's IP.
		if i := strings.Index(xff, ","); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	// RemoteAddr is host:port; strip the port.
	if i := strings.LastIndex(r.RemoteAddr, ":"); i != -1 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}