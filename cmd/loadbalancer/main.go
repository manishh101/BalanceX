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
	"intelligent-lb/internal/entrypoint"
	"intelligent-lb/internal/health"
	"intelligent-lb/internal/hotreload"
	"intelligent-lb/internal/logging"
	"intelligent-lb/internal/metrics"
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

	// Initialize dashboard
	hub := dashboard.NewHub(state.collector, "web/dashboard.html")
	hub.StartBroadcast()

	// Start terminal metrics reporter
	state.collector.StartReporter(cfg.MetricsIntervalSec)

	// ── Auto-generate TLS certs if needed ─────────────────────────────
	if cfg.TLS.Enabled && cfg.TLS.AutoGenerate {
		certFile := cfg.TLS.CertFile
		keyFile := cfg.TLS.KeyFile
		if certFile == "" {
			certFile = "server.crt"
		}
		if keyFile == "" {
			keyFile = "server.key"
		}
		if err := tlsutil.GenerateSelfSigned(certFile, keyFile); err != nil {
			log.Fatalf("[MAIN] Failed to generate self-signed certificate: %v", err)
		}
		log.Printf("[MAIN] Self-signed certificate generated: %s, %s", certFile, keyFile)
	}

	// ── Create Entrypoint Manager ─────────────────────────────────────
	// Inspired by Traefik's TCPEntryPoints pattern: each entrypoint runs
	// as its own independent HTTP server with its own goroutine, middleware
	// chain, and connection handling.
	epManager := entrypoint.NewManager()

	for epName, epCfg := range cfg.EntryPoints {
		// Resolve middleware names to actual middleware functions
		middlewares := entrypoint.ResolveMiddlewares(epCfg.Middlewares, cfg)

		var handler http.Handler

		if epName == "dashboard" {
			// Dashboard entrypoint gets the dashboard handler
			dashMux := http.NewServeMux()

			// Dashboard routes with the dashboard mux
			dashMux.Handle("/", http.HandlerFunc(hub.ServeHTTP))
			dashMux.Handle("/ws", http.HandlerFunc(hub.HandleWS))
			dashMux.Handle("/stats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(state.collector.Snapshot()); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))

			handler = dashMux
		} else {
			// All other entrypoints get the proxy handler
			handler = state.proxy
		}

		ep := entrypoint.New(epName, epCfg, handler, middlewares)
		epManager.Register(ep)
	}

	// ── Start All Entrypoints ─────────────────────────────────────────
	// Each entrypoint starts in its own goroutine — failure of one does
	// not affect others. Mirrors Traefik's TCPEntryPoints.Start().
	epManager.StartAll()

	// Print startup banner
	log.Printf("[MAIN] ═══════════════════════════════════════════════════")
	log.Printf("[MAIN] Intelligent Stateless Load Balancer")
	log.Printf("[MAIN] ═══════════════════════════════════════════════════")
	log.Printf("[MAIN] Algorithm:   %s", cfg.Algorithm)
	log.Printf("[MAIN] Servers:     %d", len(cfg.Servers))
	log.Printf("[MAIN] Health:      per-server config")
	log.Printf("[MAIN] Rate Limit:  %.0f rps/IP (burst %d)", cfg.RateLimitRPS, cfg.RateLimitBurst)
	log.Printf("[MAIN] Retry:       %d max, backoff %dms-%dms", cfg.MaxRetries, cfg.RetryBackoffMs, cfg.RetryBackoffMaxMs)
	log.Printf("[MAIN] Access Log:  %s", cfg.AccessLogPath)
	log.Printf("[MAIN] Entrypoints:")
	for name, ep := range cfg.EntryPoints {
		tlsStatus := "off"
		if ep.TLS != nil {
			tlsStatus = "enabled"
		}
		log.Printf("[MAIN]   %-12s → %s (protocol: %s, tls: %s, middlewares: %v)",
			name, ep.Address, ep.Protocol, tlsStatus, ep.Middlewares)
	}
	if cfg.HotReload {
		log.Printf("[MAIN] Hot Reload:  ENABLED")
	}
	log.Printf("[MAIN] ═══════════════════════════════════════════════════")

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
	// On SIGTERM or Ctrl+C, all entrypoints stop accepting new connections,
	// drain in-flight requests, then exit cleanly. This mirrors Traefik's
	// TCPEntryPoints.Stop() pattern with coordinated WaitGroup shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	sig := <-quit
	log.Printf("[MAIN] Received signal: %v — initiating graceful shutdown...", sig)

	// 30-second timeout for graceful shutdown of all entrypoints
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown all entrypoints concurrently
	if err := epManager.ShutdownAll(ctx); err != nil {
		log.Printf("[MAIN] Entrypoint shutdown errors: %v", err)
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
