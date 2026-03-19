package logging

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// defaultLogger writes to stdout without standard log prefixes,
// as the JSON itself handles the timestamp formatting.
var defaultLogger = log.New(os.Stdout, "", 0)

// fileLogger writes structured JSON access logs to a file.
// Initialized by InitFileLogger().
var (
	fileLogger *log.Logger
	fileMu     sync.RWMutex
)

// AccessLog represents a structured JSON log entry for load balancer access logs.
// Inspired by Traefik's access log format with all key fields for observability.
type AccessLog struct {
	Time        string  `json:"time"`
	Level       string  `json:"level"`
	Message     string  `json:"msg"`
	RequestID   string  `json:"request_id,omitempty"`
	ClientIP    string  `json:"client_ip,omitempty"`
	Method      string  `json:"method,omitempty"`
	Path        string  `json:"path,omitempty"`
	Priority    string  `json:"priority,omitempty"`
	ServerName  string  `json:"server_name,omitempty"`
	Target      string  `json:"target,omitempty"`
	StatusCode  int     `json:"status_code,omitempty"`
	LatencyMs   float64 `json:"latency_ms,omitempty"`
	Attempt     int     `json:"attempt,omitempty"`
	BackoffMs   int64   `json:"backoff_ms,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// InitFileLogger opens (or creates) the access log file for JSON line logging.
// This should be called once at startup. The file is opened in append mode.
func InitFileLogger(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	fileMu.Lock()
	fileLogger = log.New(f, "", 0)
	fileMu.Unlock()
	log.Printf("[LOGGING] Access log file initialized: %s", path)
	return nil
}

// Info logs a structured informational entry to both stdout and the access log file.
func Info(entry AccessLog) {
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if entry.Level == "" {
		entry.Level = "INFO"
	}
	b, _ := json.Marshal(entry)
	defaultLogger.Println(string(b))

	fileMu.RLock()
	fl := fileLogger
	fileMu.RUnlock()
	if fl != nil {
		fl.Println(string(b))
	}
}

// Error logs a structured error entry to both stdout and the access log file.
func Error(entry AccessLog) {
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	entry.Level = "ERROR"
	b, _ := json.Marshal(entry)
	defaultLogger.Println(string(b))

	fileMu.RLock()
	fl := fileLogger
	fileMu.RUnlock()
	if fl != nil {
		fl.Println(string(b))
	}
}
