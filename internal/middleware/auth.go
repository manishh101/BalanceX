package middleware

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuth creates a middleware that enforces HTTP Basic Authentication.
// If username or password is empty, authentication is skipped (backward compatible).
// Uses constant-time comparison to prevent timing attacks.
// Mirrors Traefik's basicAuth middleware for dashboard protection.
func BasicAuth(username, password string) Middleware {
	return func(next http.Handler) http.Handler {
		// If no credentials configured, skip auth entirely
		if username == "" || password == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 ||
				subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Load Balancer Dashboard"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
