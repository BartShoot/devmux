package daemon

import (
	"context"
	"encoding/json"
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

func TestHealthChecker_HTTP_JQ_Healthy(t *testing.T) {
	data := []map[string]interface{}{
		{"name": "git rmi bartd_git_server", "status": "OK"},
		{"name": "build rmi bartd_build_server", "status": "FAIL"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}))
	defer ts.Close()

	cfg := config.HealthCheck{
		Type:    "http",
		URL:     ts.URL,
		JQ:      `.[] | select(.name == "git rmi bartd_git_server") | .status == "OK"`,
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, err := hc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if status != StatusHealthy {
		t.Errorf("Expected Healthy, got %s", status)
	}
}

func TestHealthChecker_HTTP_JQ_Unhealthy(t *testing.T) {
	data := []map[string]interface{}{
		{"name": "build rmi bartd_build_server", "status": "FAIL"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}))
	defer ts.Close()

	cfg := config.HealthCheck{
		Type:    "http",
		URL:     ts.URL,
		JQ:      `.[] | select(.name == "build rmi bartd_build_server") | .status == "OK"`,
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, err := hc.Check(context.Background())
	if err == nil {
		t.Fatal("Expected error for unhealthy status")
	}
	if status != StatusUnhealthy {
		t.Errorf("Expected Unhealthy, got %s", status)
	}
}

func TestHealthChecker_HTTP_JQ_NoMatch(t *testing.T) {
	data := []map[string]interface{}{
		{"name": "other_server", "status": "OK"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}))
	defer ts.Close()

	cfg := config.HealthCheck{
		Type:    "http",
		URL:     ts.URL,
		JQ:      `.[] | select(.name == "missing_server") | .status == "OK"`,
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, err := hc.Check(context.Background())
	if err == nil {
		t.Fatal("Expected error when jq produces no output")
	}
	if status != StatusUnhealthy {
		t.Errorf("Expected Unhealthy, got %s", status)
	}
}

func TestHealthChecker_HTTP_JQ_InvalidExpression(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	cfg := config.HealthCheck{
		Type:    "http",
		URL:     ts.URL,
		JQ:      ".[[invalid",
		Timeout: 1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, _ := hc.Check(context.Background())
	if status != StatusUnhealthy {
		t.Errorf("Expected Unhealthy for invalid jq, got %s", status)
	}
}

func TestHealthChecker_Docker_NoDocker(t *testing.T) {
	cfg := config.HealthCheck{
		Type:      "docker",
		Container: "nonexistent_container_12345",
		Timeout:   1 * time.Second,
	}

	hc := NewHealthChecker(cfg, NewLogBuffer(10))
	status, _ := hc.Check(context.Background())
	if status != StatusUnhealthy {
		t.Errorf("Expected Unhealthy for missing container, got %s", status)
	}
}
