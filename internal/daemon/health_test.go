package daemon

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devmux/internal/config"
)

func TestHealthChecker_HTTP(t *testing.T) {
	// Mock HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := config.HealthCheck{
		Type:    "http",
		URL:     ts.URL,
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, err := hc.Check(context.Background())

	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if status != StatusHealthy {
		t.Errorf("Expected status 'Healthy', got '%s'", status)
	}
}

func TestHealthChecker_TCP(t *testing.T) {
	// Start a local TCP listener
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := config.HealthCheck{
		Type:    "tcp",
		Address: l.Addr().String(),
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, err := hc.Check(context.Background())

	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if status != StatusHealthy {
		t.Errorf("Expected status 'Healthy', got '%s'", status)
	}
}

func TestHealthChecker_Regex(t *testing.T) {
	buffer := NewLogBuffer(10)
	buffer.Write([]byte("Some log line\n"))
	buffer.Write([]byte("Server started successfully\n"))

	cfg := config.HealthCheck{
		Type:    "regex",
		Pattern: "started successfully",
	}

	hc := NewHealthChecker(cfg, buffer)
	status, err := hc.Check(context.Background())

	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if status != StatusHealthy {
		t.Errorf("Expected status 'Healthy', got '%s'", status)
	}
}
