package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
)

// requestIDKey is the context key for the request ID.
type requestIDKey struct{}

// RequestIDFromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// generateRequestID produces a UUID v4-like identifier.
func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RequestHeaders creates a middleware that enriches each request with
// standard proxy headers. This mirrors what Traefik, NGINX, and HAProxy
// set by default for downstream services:
//
//   - X-Real-IP: the original client IP
//   - X-Forwarded-For: appends the client IP to the chain
//   - X-Forwarded-Proto: "http" or "https"
//   - X-Forwarded-Host: the original Host header
//   - X-Request-ID: unique ID per request (for tracing/logging)
func RequestHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := clientIPFromRequest(r)

			// X-Real-IP: always set to the direct client
			r.Header.Set("X-Real-IP", clientIP)

			// X-Forwarded-For: append to existing chain
			if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
				r.Header.Set("X-Forwarded-For", prior+", "+clientIP)
			} else {
				r.Header.Set("X-Forwarded-For", clientIP)
			}

			// X-Forwarded-Proto
			proto := "http"
			if r.TLS != nil {
				proto = "https"
			}
			r.Header.Set("X-Forwarded-Proto", proto)

			// X-Forwarded-Host
			if r.Header.Get("X-Forwarded-Host") == "" {
				r.Header.Set("X-Forwarded-Host", r.Host)
			}

			// X-Request-ID: generate if not already present
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = generateRequestID()
				r.Header.Set("X-Request-ID", reqID)
			}

			// Store request ID in context for downstream access (logging, etc.)
			ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)

			// Echo request ID back in the response
			w.Header().Set("X-Request-ID", reqID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// clientIPFromRequest extracts the client IP from RemoteAddr, stripping the port.
func clientIPFromRequest(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
