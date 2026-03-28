package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}
	return path
}

func TestLoad_Defaults(t *testing.T) {
	path := writeTempConfig(t, `{
		"servers": [
			{"url": "http://localhost:8001", "name": "Alpha", "weight": 5}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ListenPort != 8080 {
		t.Errorf("Expected ListenPort=8080, got %d", cfg.ListenPort)
	}
	if cfg.DashboardPort != 8081 {
		t.Errorf("Expected DashboardPort=8081, got %d", cfg.DashboardPort)
	}
	if cfg.Algorithm != "weighted" {
		t.Errorf("Expected Algorithm='weighted', got %q", cfg.Algorithm)
	}
	if cfg.HealthInterval != 5 {
		t.Errorf("Expected HealthInterval=5, got %d", cfg.HealthInterval)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries=3, got %d", cfg.MaxRetries)
	}
	if cfg.RateLimitRPS != 100 {
		t.Errorf("Expected RateLimitRPS=100, got %f", cfg.RateLimitRPS)
	}
	if cfg.Timeouts.HighSec != 5 {
		t.Errorf("Expected Timeouts.HighSec=5, got %d", cfg.Timeouts.HighSec)
	}
}

func TestLoad_BackwardCompatibility_LegacyServers(t *testing.T) {
	path := writeTempConfig(t, `{
		"servers": [
			{"url": "http://localhost:8001", "name": "Alpha", "weight": 5},
			{"url": "http://localhost:8002", "name": "Beta", "weight": 3}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Legacy servers should be wrapped into "default" service
	if len(cfg.Services) != 1 {
		t.Fatalf("Expected 1 service ('default'), got %d", len(cfg.Services))
	}
	defaultSvc, ok := cfg.Services["default"]
	if !ok {
		t.Fatal("Expected 'default' service to exist")
	}
	if len(defaultSvc.Servers) != 2 {
		t.Errorf("Expected 2 servers in default service, got %d", len(defaultSvc.Servers))
	}
}

func TestLoad_BackwardCompatibility_Entrypoints(t *testing.T) {
	path := writeTempConfig(t, `{
		"listen_port": 9090,
		"dashboard_port": 9091,
		"servers": [
			{"url": "http://localhost:8001", "name": "Alpha", "weight": 1}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Entrypoints should be synthesized from legacy ports
	if len(cfg.EntryPoints) != 2 {
		t.Fatalf("Expected 2 entrypoints, got %d", len(cfg.EntryPoints))
	}
	webEP, ok := cfg.EntryPoints["web"]
	if !ok {
		t.Fatal("Expected 'web' entrypoint")
	}
	if webEP.Address != ":9090" {
		t.Errorf("Expected web address ':9090', got %q", webEP.Address)
	}
	dashEP, ok := cfg.EntryPoints["dashboard"]
	if !ok {
		t.Fatal("Expected 'dashboard' entrypoint")
	}
	if dashEP.Address != ":9091" {
		t.Errorf("Expected dashboard address ':9091', got %q", dashEP.Address)
	}
}

func TestLoad_ServiceHealthCheckDefaults(t *testing.T) {
	path := writeTempConfig(t, `{
		"services": {
			"api": {
				"servers": [
					{"url": "http://localhost:8001", "name": "Alpha", "weight": 1}
				]
			}
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	svc := cfg.Services["api"]
	if svc.HealthCheck.Path != "/health" {
		t.Errorf("Expected health path '/health', got %q", svc.HealthCheck.Path)
	}
	if svc.CircuitBreaker.Threshold != 3 {
		t.Errorf("Expected breaker threshold 3, got %d", svc.CircuitBreaker.Threshold)
	}
	if svc.LoadBalancer.Algorithm != "weighted" {
		t.Errorf("Expected algorithm 'weighted', got %q", svc.LoadBalancer.Algorithm)
	}
	// Server-level defaults
	if svc.Servers[0].Weight != 1 {
		t.Errorf("Expected server weight 1, got %d", svc.Servers[0].Weight)
	}
}

func TestValidate_DuplicateURLsAcrossServices(t *testing.T) {
	path := writeTempConfig(t, `{
		"services": {
			"svc-a": {
				"servers": [{"url": "http://localhost:8001", "name": "A", "weight": 1}]
			},
			"svc-b": {
				"servers": [{"url": "http://localhost:8001", "name": "B", "weight": 1}]
			}
		}
	}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Expected validation error for duplicate URL across services, got nil")
	}
}

func TestValidate_UniqueURLsPass(t *testing.T) {
	path := writeTempConfig(t, `{
		"services": {
			"svc-a": {
				"servers": [{"url": "http://localhost:8001", "name": "A", "weight": 1}]
			},
			"svc-b": {
				"servers": [{"url": "http://localhost:8002", "name": "B", "weight": 1}]
			}
		}
	}`)

	_, err := Load(path)
	if err != nil {
		t.Fatalf("Expected no error for unique URLs, got: %v", err)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	path := writeTempConfig(t, `{invalid json}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
}
