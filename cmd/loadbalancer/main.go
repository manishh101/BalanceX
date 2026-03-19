package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"intelligent-lb/config"
	"intelligent-lb/internal/balancer"
	"intelligent-lb/internal/dashboard"
	"intelligent-lb/internal/health"
	"intelligent-lb/internal/hotreload"
	"intelligent-lb/internal/logging"
	"intelligent-lb/internal/metrics"
	"intelligent-lb/internal/middleware"
	"intelligent-lb/internal/proxy"
	"intelligent-lb/internal/tlsutil"
)

// appState holds all mutable components that can be updated during hot reload.
type appState struct {
	mu        sync.RWMutex
	cfg       *config.Config
	router    *balancer.Router
	collector *metrics.Collector
	breakers  map[string]*health.Breaker
	monitor   *health.Monitor
	proxy     *proxy.Handler
}

func main() {
	// Load configuration
	cfg, err := config.Load("config/config.json")
	if err != nil {
		log.Fatalf("[MAIN] Failed to load config: %v", err)
	}

	// Initialize structured access log file
	if err := logging.InitFileLogger(cfg.AccessLogPath); err != nil {
		log.Printf("[MAIN] Warning: failed to initialize access log file: %v", err)
	}

	// Build the application state
	state := &appState{cfg: cfg}
	state.initialize(cfg)

	// ── Build Middleware Pipeline ──────────────────────────────────────
	// Mirrors Traefik's composable middleware chain:
	// Request → Headers → CORS → PerIP RateLimit → Proxy → Response
	ipRateLimiter := middleware.NewPerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)

	corsConfig := middleware.DefaultCORSConfig()
	if len(cfg.CORS.AllowedOrigins) > 0 {
		corsConfig.AllowedOrigins = cfg.CORS.AllowedOrigins
	}
	if len(cfg.CORS.AllowedMethods) > 0 {
		corsConfig.AllowedMethods = cfg.CORS.AllowedMethods
	}
	if len(cfg.CORS.AllowedHeaders) > 0 {
		corsConfig.AllowedHeaders = cfg.CORS.AllowedHeaders
	}

	chain := middleware.Chain(
		middleware.RequestHeaders(),               // 1. Enrich X-Forwarded-For, X-Real-IP, X-Request-ID
		middleware.CORS(corsConfig),                // 2. CORS headers + preflight handling
		ipRateLimiter.Middleware(),                 // 3. Per-IP rate limiting (429)
	)

	// Wrap proxy with middleware chain
	handler := chain(state.proxy)

	// Initialize dashboard
	hub := dashboard.NewHub(state.collector, "web/dashboard.html")
	hub.StartBroadcast()

	// Start terminal metrics reporter
	state.collector.StartReporter(cfg.MetricsIntervalSec)

	// ── Dashboard Server (background) ──────────────────────────────────
	dashMux := http.NewServeMux()

	// Wrap dashboard with Basic Auth if configured
	authMiddleware := middleware.BasicAuth(cfg.DashboardAuth.Username, cfg.DashboardAuth.Password)
	dashMux.Handle("/", authMiddleware(http.HandlerFunc(hub.ServeHTTP)))
	dashMux.Handle("/ws", authMiddleware(http.HandlerFunc(hub.HandleWS)))
	dashMux.Handle("/stats", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state.collector.Snapshot()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})))

	dashAddr := fmt.Sprintf(":%d", cfg.DashboardPort)
	dashServer := &http.Server{
		Addr:    dashAddr,
		Handler: dashMux,
	}
	go func() {
		log.Printf("[MAIN] Dashboard server starting on %s", dashAddr)
		if cfg.DashboardAuth.Username != "" {
			log.Printf("[MAIN] Dashboard authentication: ENABLED")
		}
		if err := dashServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[MAIN] Dashboard server failed: %v", err)
		}
	}()

	// ── Load Balancer Proxy Server ─────────────────────────────────────
	lbAddr := fmt.Sprintf(":%d", cfg.ListenPort)
	lbServer := &http.Server{
		Addr:         lbAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[MAIN] ═══════════════════════════════════════════════════")
		log.Printf("[MAIN] Intelligent Stateless Load Balancer")
		log.Printf("[MAIN] ═══════════════════════════════════════════════════")
		log.Printf("[MAIN] Listen:      %s", lbAddr)
		log.Printf("[MAIN] Algorithm:   %s", cfg.Algorithm)
		log.Printf("[MAIN] Servers:     %d", len(cfg.Servers))
		log.Printf("[MAIN] Health:      per-server config")
		log.Printf("[MAIN] Dashboard:   http://localhost:%d", cfg.DashboardPort)
		log.Printf("[MAIN] Rate Limit:  %.0f rps/IP (burst %d)", cfg.RateLimitRPS, cfg.RateLimitBurst)
		log.Printf("[MAIN] Retry:       %d max, backoff %dms-%dms", cfg.MaxRetries, cfg.RetryBackoffMs, cfg.RetryBackoffMaxMs)
		log.Printf("[MAIN] Access Log:  %s", cfg.AccessLogPath)
		if cfg.TLS.Enabled {
			log.Printf("[MAIN] TLS:         ENABLED")
		}
		if cfg.HotReload {
			log.Printf("[MAIN] Hot Reload:  ENABLED")
		}
		log.Printf("[MAIN] ═══════════════════════════════════════════════════")

		if cfg.TLS.Enabled {
			// Handle TLS
			certFile := cfg.TLS.CertFile
			keyFile := cfg.TLS.KeyFile
			if certFile == "" {
				certFile = "server.crt"
			}
			if keyFile == "" {
				keyFile = "server.key"
			}

			// Auto-generate self-signed cert if requested
			if cfg.TLS.AutoGenerate {
				if err := tlsutil.GenerateSelfSigned(certFile, keyFile); err != nil {
					log.Fatalf("[MAIN] Failed to generate self-signed certificate: %v", err)
				}
				log.Printf("[MAIN] Self-signed certificate generated: %s, %s", certFile, keyFile)
			}

			log.Printf("[MAIN] Starting HTTPS server on %s", lbAddr)
			if err := lbServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[MAIN] Load balancer (TLS) failed: %v", err)
			}
		} else {
			if err := lbServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[MAIN] Load balancer failed: %v", err)
			}
		}
	}()

	// ── Config Hot Reload ──────────────────────────────────────────────
	if cfg.HotReload {
		_, err := hotreload.NewWatcher("config/config.json", func(path string) error {
			return state.reload(path)
		})
		if err != nil {
			log.Printf("[MAIN] Warning: hot reload failed to start: %v", err)
		}
	}

	// ── Graceful Shutdown ──────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	sig := <-quit
	log.Printf("[MAIN] Received signal: %v — initiating graceful shutdown...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSec)*time.Second)
	defer cancel()

	if err := lbServer.Shutdown(ctx); err != nil {
		log.Printf("[MAIN] LB server forced shutdown: %v", err)
	}

	if err := dashServer.Shutdown(ctx); err != nil {
		log.Printf("[MAIN] Dashboard server forced shutdown: %v", err)
	}

	state.monitor.Stop()

	log.Println("[MAIN] Graceful shutdown complete. Goodbye!")
}

// initialize sets up all components from the given config.
func (s *appState) initialize(cfg *config.Config) {
	var serverURLs []string
	var serverNames []string
	var serverWeights []int
	for _, srv := range cfg.Servers {
		serverURLs = append(serverURLs, srv.URL)
		serverNames = append(serverNames, srv.Name)
		serverWeights = append(serverWeights, srv.Weight)
	}

	s.collector = metrics.New(serverURLs, serverNames, serverWeights)

	s.breakers = make(map[string]*health.Breaker)
	for _, url := range serverURLs {
		s.breakers[url] = health.NewBreaker(
			cfg.BreakerThreshold,
			time.Duration(cfg.BreakerTimeoutSec)*time.Second,
		)
	}

	var algo balancer.Algorithm
	switch cfg.Algorithm {
	case "roundrobin":
		algo = &balancer.RoundRobin{}
	case "leastconn":
		algo = balancer.LeastConnections{}
	case "canary":
		algo = &balancer.Canary{}
	default:
		algo = balancer.WeightedScore{}
	}

	s.router = balancer.NewRouter(serverURLs, s.collector, s.breakers, algo)

	s.monitor = health.NewMonitor(cfg.Servers, s.collector, s.breakers)
	s.monitor.Start()

	s.proxy = proxy.New(
		s.router, s.collector, s.breakers,
		cfg.MaxRetries, cfg.PerAttemptTimeoutSec,
		cfg.RetryBackoffMs, cfg.RetryBackoffMaxMs,
	)
}

// reload re-reads the config and swaps out mutable components.
// This is called by the hot reload watcher when config.json changes.
func (s *appState) reload(path string) error {
	newCfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing health monitor
	s.monitor.Stop()

	// Reinitialize with new config
	s.cfg = newCfg
	s.initialize(newCfg)

	log.Printf("[MAIN] Hot reload complete: %d servers, algorithm=%s", len(newCfg.Servers), newCfg.Algorithm)
	return nil
}
