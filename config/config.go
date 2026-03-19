package config

import (
	"encoding/json"
	"os"
)

// HealthCheckConfig holds per-server health check settings.
// All fields are optional — sensible defaults are applied from the global config.
type HealthCheckConfig struct {
	Path           string `json:"path,omitempty"`            // Health check endpoint path (default: "/health")
	IntervalSec    int    `json:"interval_sec,omitempty"`    // Check interval in seconds (default: global health_interval_sec)
	TimeoutSec     int    `json:"timeout_sec,omitempty"`     // HTTP timeout for health check (default: 2)
	ExpectedStatus int    `json:"expected_status,omitempty"` // Expected HTTP status code (default: 200)
}

// ServerConfig holds configuration for a single backend server.
type ServerConfig struct {
	URL         string            `json:"url"`
	Name        string            `json:"name"`
	Weight      int               `json:"weight"`
	DelayMs     int               `json:"delay_ms"`
	HealthCheck HealthCheckConfig `json:"health_check,omitempty"` // Per-server health check config
}

// DashboardAuth holds basic authentication credentials for the dashboard.
// If both fields are empty, authentication is disabled (backward compatible).
type DashboardAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// TLSConfig holds TLS/HTTPS configuration.
// If Enabled is false (default), the load balancer serves plain HTTP.
type TLSConfig struct {
	Enabled      bool   `json:"enabled,omitempty"`
	CertFile     string `json:"cert_file,omitempty"`
	KeyFile      string `json:"key_file,omitempty"`
	AutoGenerate bool   `json:"auto_generate,omitempty"` // Generate self-signed cert if files don't exist
}

// CORSConfig holds CORS configuration for the middleware.
type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
	AllowedMethods []string `json:"allowed_methods,omitempty"`
	AllowedHeaders []string `json:"allowed_headers,omitempty"`
}

// Config holds the entire load balancer configuration.
type Config struct {
	ListenPort         int            `json:"listen_port"`
	DashboardPort      int            `json:"dashboard_port"`
	Servers            []ServerConfig `json:"servers"`
	Algorithm          string         `json:"algorithm"`
	HealthInterval     int            `json:"health_interval_sec"`
	BreakerThreshold   int            `json:"breaker_threshold"`
	BreakerTimeoutSec  int            `json:"breaker_timeout_sec"`
	MetricsIntervalSec int            `json:"metrics_interval_sec"`
	MaxRetries         int            `json:"max_retries"`
	ShutdownTimeoutSec int            `json:"shutdown_timeout_sec"`
	RateLimitRPS       float64        `json:"rate_limit_rps"`
	RateLimitBurst     int            `json:"rate_limit_burst"`
	PerAttemptTimeoutSec int          `json:"per_attempt_timeout_sec"`

	// New fields — all backward compatible with zero-value defaults
	RetryBackoffMs    int           `json:"retry_backoff_ms,omitempty"`
	RetryBackoffMaxMs int           `json:"retry_backoff_max_ms,omitempty"`
	AccessLogPath     string        `json:"access_log_path,omitempty"`
	DashboardAuth     DashboardAuth `json:"dashboard_auth,omitempty"`
	TLS               TLSConfig     `json:"tls,omitempty"`
	CORS              CORSConfig    `json:"cors,omitempty"`
	HotReload         bool          `json:"hot_reload,omitempty"`
}

// Load reads and parses a JSON configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	setDefaults(&cfg)
	return &cfg, nil
}

// setDefaults applies sensible defaults for any unset config values.
func setDefaults(cfg *Config) {
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 8080
	}
	if cfg.DashboardPort == 0 {
		cfg.DashboardPort = 8081
	}
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = 5
	}
	if cfg.BreakerThreshold == 0 {
		cfg.BreakerThreshold = 3
	}
	if cfg.BreakerTimeoutSec == 0 {
		cfg.BreakerTimeoutSec = 15
	}
	if cfg.MetricsIntervalSec == 0 {
		cfg.MetricsIntervalSec = 10
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = "weighted"
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.ShutdownTimeoutSec == 0 {
		cfg.ShutdownTimeoutSec = 15
	}
	if cfg.RateLimitRPS == 0 {
		cfg.RateLimitRPS = 100 // per-IP: 100 requests per second
	}
	if cfg.RateLimitBurst == 0 {
		cfg.RateLimitBurst = 200
	}
	if cfg.PerAttemptTimeoutSec == 0 {
		cfg.PerAttemptTimeoutSec = 5
	}
	if cfg.RetryBackoffMs == 0 {
		cfg.RetryBackoffMs = 100
	}
	if cfg.RetryBackoffMaxMs == 0 {
		cfg.RetryBackoffMaxMs = 5000
	}
	if cfg.AccessLogPath == "" {
		cfg.AccessLogPath = "access.log"
	}

	// Apply per-server health check defaults
	for i := range cfg.Servers {
		s := &cfg.Servers[i]
		if s.HealthCheck.Path == "" {
			s.HealthCheck.Path = "/health"
		}
		if s.HealthCheck.IntervalSec == 0 {
			s.HealthCheck.IntervalSec = cfg.HealthInterval
		}
		if s.HealthCheck.TimeoutSec == 0 {
			s.HealthCheck.TimeoutSec = 2
		}
		if s.HealthCheck.ExpectedStatus == 0 {
			s.HealthCheck.ExpectedStatus = 200
		}
	}
}
