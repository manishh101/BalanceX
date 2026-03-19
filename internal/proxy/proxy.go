package proxy

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"time"

	"intelligent-lb/internal/balancer"
	"intelligent-lb/internal/health"
	"intelligent-lb/internal/logging"
	"intelligent-lb/internal/metrics"
	"intelligent-lb/internal/middleware"
	"intelligent-lb/internal/priority"
)

// Handler is the HTTP reverse proxy that routes requests to backend servers.
// It uses an optimized transport for high-throughput connection pooling
// and implements transparent retry logic with exponential backoff.
type Handler struct {
	router            *balancer.Router
	metrics           *metrics.Collector
	breakers          map[string]*health.Breaker
	client            *http.Client
	maxRetries        int
	perAttemptTimeout time.Duration
	initialBackoff    time.Duration
	maxBackoff        time.Duration
}

// New creates a new proxy Handler with a production-grade HTTP transport.
// The transport is tuned for high concurrency with aggressive connection
// pooling, matching patterns used in Envoy and NGINX proxy backends.
func New(r *balancer.Router, m *metrics.Collector, b map[string]*health.Breaker,
	maxRetries int, perAttemptTimeoutSec int,
	initialBackoffMs int, maxBackoffMs int,
) *Handler {
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	return &Handler{
		router:            r,
		metrics:           m,
		breakers:          b,
		maxRetries:        maxRetries,
		perAttemptTimeout: time.Duration(perAttemptTimeoutSec) * time.Second,
		initialBackoff:    time.Duration(initialBackoffMs) * time.Millisecond,
		maxBackoff:        time.Duration(maxBackoffMs) * time.Millisecond,
		client: &http.Client{
			Transport: transport,
		},
	}
}

// ServeHTTP handles each incoming request by classifying its priority,
// selecting a backend with retry logic and exponential backoff, proxying
// the request, and recording metrics.
//
// Retry with exponential backoff (inspired by Traefik's retry middleware):
//   - On backend failure, wait initialBackoff * 2^attempt + jitter before retrying
//   - Backoff is capped at maxBackoff to prevent excessive delays
//   - Client context cancellation aborts the backoff sleep immediately
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Extract request ID and client IP from middleware-enriched headers
	requestID := middleware.RequestIDFromContext(req.Context())
	clientIP := req.Header.Get("X-Real-IP")
	if clientIP == "" {
		clientIP = req.RemoteAddr
	}

	// Step 1: Classify request priority
	pri := priority.Classify(req.URL.Path, req.Header.Get("X-Priority"))

	// Step 2: Retry loop with exponential backoff
	var lastErr error
	tried := make(map[string]bool)

	for attempt := 0; attempt < h.maxRetries; attempt++ {
		// Exponential backoff before retry (skip on first attempt)
		if attempt > 0 {
			backoff := h.calculateBackoff(attempt)
			logging.Info(logging.AccessLog{
				Message:   "Retrying with exponential backoff",
				RequestID: requestID,
				ClientIP:  clientIP,
				Method:    req.Method,
				Path:      req.URL.Path,
				Attempt:   attempt + 1,
				BackoffMs: backoff.Milliseconds(),
			})

			// Sleep with context cancellation support
			select {
			case <-time.After(backoff):
				// Backoff complete, proceed with retry
			case <-req.Context().Done():
				http.Error(w, "Client disconnected during retry backoff", http.StatusGatewayTimeout)
				return
			}
		}

		target, err := h.router.Select(pri)
		if err != nil {
			lastErr = err
			break
		}

		if tried[target] {
			continue
		}
		tried[target] = true

		success, done := h.proxyToBackend(w, req, target, pri, attempt, requestID, clientIP)
		if done {
			return
		}
		if !success {
			lastErr = fmt.Errorf("backend %s failed", target)
			logging.Error(logging.AccessLog{
				Message:   "Backend failed, trying next server",
				RequestID: requestID,
				ClientIP:  clientIP,
				Method:    req.Method,
				Path:      req.URL.Path,
				Target:    target,
				Attempt:   attempt + 1,
			})
			continue
		}
		return
	}

	// All retries exhausted
	http.Error(w, "All backend servers unavailable: "+lastErr.Error(), http.StatusBadGateway)
	logging.Error(logging.AccessLog{
		Message:   "ALL RETRIES EXHAUSTED - 502 returned to client",
		RequestID: requestID,
		ClientIP:  clientIP,
		Method:    req.Method,
		Path:      req.URL.Path,
		Priority:  pri,
		Error:     lastErr.Error(),
	})
}

// calculateBackoff computes the exponential backoff duration for the given attempt.
// Formula: min(initialBackoff * 2^attempt + jitter, maxBackoff)
// Jitter is 0-25% of the calculated delay to prevent thundering herd.
func (h *Handler) calculateBackoff(attempt int) time.Duration {
	backoff := float64(h.initialBackoff) * math.Pow(2, float64(attempt-1))
	if backoff > float64(h.maxBackoff) {
		backoff = float64(h.maxBackoff)
	}
	// Add jitter: 0-25% of the calculated backoff
	jitter := rand.Float64() * 0.25 * backoff
	return time.Duration(backoff + jitter)
}

// proxyToBackend forwards a single request to the given target server.
func (h *Handler) proxyToBackend(
	w http.ResponseWriter,
	req *http.Request,
	target, pri string,
	attempt int,
	requestID, clientIP string,
) (success bool, responseSent bool) {
	h.metrics.RecordStart(target)
	start := time.Now()

	ctx, cancel := context.WithTimeout(req.Context(), h.perAttemptTimeout)
	defer cancel()

	url := fmt.Sprintf("%s%s", target, req.RequestURI)
	proxyReq, err := http.NewRequestWithContext(ctx, req.Method, url, req.Body)
	if err != nil {
		h.metrics.RecordEnd(target, 0, false)
		http.Error(w, "Failed to build proxy request", http.StatusInternalServerError)
		return false, true
	}

	// Copy all original headers (including enriched headers from middleware)
	for k, vals := range req.Header {
		proxyReq.Header[k] = vals
	}

	// Execute the request to the backend
	resp, err := h.client.Do(proxyReq)
	latencyMs := float64(time.Since(start).Milliseconds())

	if err != nil {
		h.metrics.RecordEnd(target, latencyMs, false)
		h.breakers[target].RecordFailure()
		h.metrics.SetCircuitState(target, h.breakers[target].State())

		logging.Error(logging.AccessLog{
			Message:   "Proxy request failed",
			RequestID: requestID,
			ClientIP:  clientIP,
			Method:    req.Method,
			Path:      req.URL.Path,
			Priority:  pri,
			Target:    target,
			LatencyMs: latencyMs,
			Attempt:   attempt + 1,
			Error:     err.Error(),
		})

		return false, false
	}
	defer resp.Body.Close()

	isSuccess := resp.StatusCode < 500
	h.metrics.RecordEnd(target, latencyMs, isSuccess)

	if isSuccess {
		h.breakers[target].RecordSuccess()
	} else {
		h.breakers[target].RecordFailure()
	}
	h.metrics.SetCircuitState(target, h.breakers[target].State())
	h.metrics.RecordPriority(target, pri)

	// If backend returned 5xx and we haven't exhausted retries, signal for retry
	if !isSuccess && attempt < h.maxRetries-1 {
		io.Copy(io.Discard, resp.Body)

		logging.Error(logging.AccessLog{
			Message:    "Proxy returned 5xx, will retry",
			RequestID:  requestID,
			ClientIP:   clientIP,
			Method:     req.Method,
			Path:       req.URL.Path,
			Priority:   pri,
			Target:     target,
			StatusCode: resp.StatusCode,
			LatencyMs:  latencyMs,
			Attempt:    attempt + 1,
		})

		return false, false
	}

	// Write the response to the client
	serverName := h.metrics.GetName(target)
	for k, vals := range resp.Header {
		w.Header()[k] = vals
	}
	w.Header().Set("X-Handled-By", serverName)
	w.Header().Set("X-Retry-Count", fmt.Sprintf("%d", attempt))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	logging.Info(logging.AccessLog{
		Message:    "Request proxied successfully",
		RequestID:  requestID,
		ClientIP:   clientIP,
		Method:     req.Method,
		Path:       req.URL.Path,
		Priority:   pri,
		ServerName: serverName,
		Target:     target,
		StatusCode: resp.StatusCode,
		LatencyMs:  latencyMs,
		Attempt:    attempt + 1,
	})

	return isSuccess, true
}
