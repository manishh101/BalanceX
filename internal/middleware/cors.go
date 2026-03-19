package middleware

import (
	"net/http"
	"strings"
)

// CORSConfig holds CORS middleware configuration.
type CORSConfig struct {
	AllowedOrigins []string // e.g. ["*"] or ["https://example.com"]
	AllowedMethods []string // e.g. ["GET", "POST", "PUT", "DELETE"]
	AllowedHeaders []string // e.g. ["Content-Type", "Authorization"]
	MaxAge         string   // preflight cache duration, e.g. "3600"
}

// DefaultCORSConfig returns a permissive CORS config suitable for development.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-Requested-With", "X-Priority"},
		MaxAge:         "3600",
	}
}

// CORS creates a CORS middleware that sets appropriate headers on all responses
// and handles preflight OPTIONS requests. Mirrors Traefik's headers middleware CORS support.
func CORS(cfg CORSConfig) Middleware {
	origins := strings.Join(cfg.AllowedOrigins, ", ")
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origins)
			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			w.Header().Set("Access-Control-Max-Age", cfg.MaxAge)

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
