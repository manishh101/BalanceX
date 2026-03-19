package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter holds a per-IP rate limiter and the last time it was accessed.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// PerIPRateLimiter enforces per-client-IP rate limiting.
// Unlike a global rate limiter, this ensures one misbehaving client
// cannot exhaust the budget for all clients — matching Traefik's rateLimit middleware.
type PerIPRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*ipLimiter
	rps      rate.Limit
	burst    int
}

// NewPerIPRateLimiter creates a per-IP rate limiter with the given RPS and burst.
// A background goroutine cleans up stale entries every 3 minutes.
func NewPerIPRateLimiter(rps float64, burst int) *PerIPRateLimiter {
	rl := &PerIPRateLimiter{
		limiters: make(map[string]*ipLimiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go rl.cleanup()
	return rl
}

// getLimiter returns the rate.Limiter for the given IP, creating one if needed.
func (rl *PerIPRateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if entry, ok := rl.limiters[ip]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[ip] = &ipLimiter{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	return limiter
}

// cleanup removes IP entries that haven't been seen in 5 minutes.
func (rl *PerIPRateLimiter) cleanup() {
	ticker := time.NewTicker(3 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		for ip, entry := range rl.limiters {
			if time.Since(entry.lastSeen) > 5*time.Minute {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns a Middleware that enforces per-IP rate limiting.
// Returns HTTP 429 Too Many Requests with Retry-After header when exceeded.
func (rl *PerIPRateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			limiter := rl.getLimiter(ip)

			if !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractIP gets the client IP address, checking X-Real-IP and
// X-Forwarded-For headers first (set by upstream proxies), then
// falling back to RemoteAddr.
func extractIP(r *http.Request) string {
	// Check X-Real-IP first (most reliable when set by a trusted proxy)
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// Check X-Forwarded-For (first IP is the original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Fall back to RemoteAddr (strip port)
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
