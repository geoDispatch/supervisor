package auth

import (
	"context"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/geodispatch/supervisor/internal/models"
)

// contextKey is unexported so only this package can read auth values from ctx.
type contextKey int

const claimsKey contextKey = 0

// ClaimsFromContext returns the JWT claims stored by JWTMiddleware, or nil.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

// JWTMiddleware validates the Authorization: Bearer <token> header.
// On success it stores the claims in the request context and calls next.
// On failure it returns 401 — no next call.
func JWTMiddleware(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			writeUnauthorized(w, "missing_token")
			return
		}
		claims, err := VerifyToken(secret, strings.TrimPrefix(raw, "Bearer "))
		if err != nil {
			writeUnauthorized(w, "invalid_token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIKeyMiddleware validates the X-API-Key header against the api_keys table.
// On success it calls next. On failure it returns 401.
func APIKeyMiddleware(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("X-API-Key")
		if raw == "" {
			writeUnauthorized(w, "missing_api_key")
			return
		}

		// Load all hashes and check with bcrypt. The api_keys table is small
		// (a handful of rows per user) so a full scan is fine. If it grows,
		// add a prefix column for a fast pre-filter.
		var keys []models.APIKey
		if err := db.Find(&keys).Error; err != nil {
			writeJSON401(w, "server_error")
			return
		}
		for _, k := range keys {
			if CheckPassword(k.KeyHash, raw) == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeUnauthorized(w, "invalid_api_key")
	})
}

// JWTOrAPIKey allows a request that satisfies either credential.
// JWT is checked first; if that header is absent, API key is tried.
func JWTOrAPIKey(secret string, db *gorm.DB, next http.Handler) http.Handler {
	jwtH := JWTMiddleware(secret, next)
	keyH := APIKeyMiddleware(db, next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			jwtH.ServeHTTP(w, r)
			return
		}
		keyH.ServeHTTP(w, r)
	})
}

func writeUnauthorized(w http.ResponseWriter, code string) {
	writeJSON401(w, code)
}

func writeJSON401(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="geodispatch"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + code + `"}`))
}