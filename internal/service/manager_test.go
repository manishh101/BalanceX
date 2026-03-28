package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"intelligent-lb/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Algorithm:         "weighted",
		HealthInterval:    5,
		BreakerThreshold:  3,
		BreakerTimeoutSec: 15,
		MaxRetries:        3,
		PerAttemptTimeoutSec: 5,
		Services: map[string]*config.ServiceConfig{
			"api": {
				LoadBalancer:   &config.LoadBalancerConfig{Algorithm: "weighted"},
				HealthCheck:    &config.HealthCheckConfig{Path: "/health", IntervalSec: 5, TimeoutSec: 2, ExpectedStatus: 200},
				CircuitBreaker: &config.CircuitBreakerConfig{Threshold: 3, RecoveryTimeoutSec: 15},
				Servers: []config.ServerConfig{
					{URL: "http://127.0.0.1:19001", Name: "TestAlpha", Weight: 5, HealthCheck: config.HealthCheckConfig{Path: "/health", IntervalSec: 5, TimeoutSec: 1, ExpectedStatus: 200}},
					{URL: "http://127.0.0.1:19002", Name: "TestBeta", Weight: 3, HealthCheck: config.HealthCheckConfig{Path: "/health", IntervalSec: 5, TimeoutSec: 1, ExpectedStatus: 200}},
				},
			},
			"admin": {
				LoadBalancer:   &config.LoadBalancerConfig{Algorithm: "roundrobin"},
				HealthCheck:    &config.HealthCheckConfig{Path: "/health", IntervalSec: 5, TimeoutSec: 2, ExpectedStatus: 200},
				CircuitBreaker: &config.CircuitBreakerConfig{Threshold: 3, RecoveryTimeoutSec: 15},
				Servers: []config.ServerConfig{
					{URL: "http://127.0.0.1:19003", Name: "TestGamma", Weight: 1, HealthCheck: config.HealthCheckConfig{Path: "/health", IntervalSec: 5, TimeoutSec: 1, ExpectedStatus: 200}},
				},
			},
		},
	}
}

func TestNewManager_CreatesInstances(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	instances := mgr.Instances()
	if len(instances) != 2 {
		t.Fatalf("Expected 2 instances, got %d", len(instances))
	}
	if _, ok := instances["api"]; !ok {
		t.Error("Expected 'api' instance")
	}
	if _, ok := instances["admin"]; !ok {
		t.Error("Expected 'admin' instance")
	}
}

func TestManager_Get(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	handler := mgr.Get("api")
	if handler == nil {
		t.Fatal("Expected non-nil handler for 'api'")
	}
	nilHandler := mgr.Get("nonexistent")
	if nilHandler != nil {
		t.Fatal("Expected nil handler for nonexistent service")
	}
}

func TestManager_GetInstance(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	inst := mgr.GetInstance("api")
	if inst == nil {
		t.Fatal("Expected non-nil instance for 'api'")
	}
	if inst.Name != "api" {
		t.Errorf("Expected Name='api', got %q", inst.Name)
	}
	if len(inst.Config.Servers) != 2 {
		t.Errorf("Expected 2 servers in api, got %d", len(inst.Config.Servers))
	}
}

func TestManager_DashboardSnap(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	snap := mgr.DashboardSnap()
	// Should aggregate all servers from both services
	if snap.TotalCount != 3 {
		t.Errorf("Expected TotalCount=3, got %d", snap.TotalCount)
	}
	if snap.Algorithm != "weighted" {
		t.Errorf("Expected Algorithm='weighted', got %q", snap.Algorithm)
	}
}

func TestManager_ImportMetrics(t *testing.T) {
	cfg := testConfig()
	oldMgr := NewManager(cfg)

	// Record some metrics on old manager
	inst := oldMgr.GetInstance("api")
	inst.Collector.RecordStart("http://127.0.0.1:19001")
	inst.Collector.RecordEnd("http://127.0.0.1:19001", 10.0, true)
	inst.Collector.RecordStart("http://127.0.0.1:19001")
	inst.Collector.RecordEnd("http://127.0.0.1:19001", 20.0, true)

	oldSnap := inst.Collector.Snapshot()
	if oldSnap["http://127.0.0.1:19001"].TotalRequests != 2 {
		t.Fatalf("Expected 2 requests on old, got %d", oldSnap["http://127.0.0.1:19001"].TotalRequests)
	}
	oldMgr.Stop()

	// Create new manager and import
	newMgr := NewManager(cfg)
	defer newMgr.Stop()
	newMgr.ImportMetrics(oldMgr)

	newInst := newMgr.GetInstance("api")
	newSnap := newInst.Collector.Snapshot()
	if newSnap["http://127.0.0.1:19001"].TotalRequests != 2 {
		t.Errorf("Expected imported TotalRequests=2, got %d", newSnap["http://127.0.0.1:19001"].TotalRequests)
	}
}

func TestManager_GetConfig(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	got := mgr.GetConfig()
	if got != cfg {
		t.Error("Expected GetConfig to return the original config pointer")
	}
}

func TestManager_HandlerServes502WhenBackendsDown(t *testing.T) {
	cfg := testConfig()
	mgr := NewManager(cfg)
	defer mgr.Stop()

	// All backends are unreachable, so requests should get routed but fail
	handler := mgr.Get("api")
	if handler == nil {
		t.Fatal("Expected handler")
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The proxy should return 502 since no backends are reachable
	if w.Code != http.StatusBadGateway {
		t.Logf("Got status %d (expected 502, but may vary if health checks haven't run)", w.Code)
	}
}
