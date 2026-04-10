package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/itchyny/gojq"

	"devmux/internal/config"
)

type HealthStatus string

const (
	StatusHealthy   HealthStatus = "Healthy"
	StatusUnhealthy HealthStatus = "Unhealthy"
	StatusChecking  HealthStatus = "Checking"
)

type HealthChecker struct {
	config config.HealthCheck
	buffer *LogBuffer
}

func NewHealthChecker(cfg config.HealthCheck, buffer *LogBuffer) *HealthChecker {
	return &HealthChecker{
		config: cfg,
		buffer: buffer,
	}
}

func (hc *HealthChecker) Check(ctx context.Context) (HealthStatus, error) {
	switch hc.config.Type {
	case "http":
		return hc.checkHTTP(ctx)
	case "tcp":
		return hc.checkTCP(ctx)
	case "regex":
		return hc.checkRegex(ctx)
	case "docker":
		return hc.checkDocker(ctx)
	default:
		return StatusUnhealthy, fmt.Errorf("unknown health check type: %s", hc.config.Type)
	}
}

func (hc *HealthChecker) checkHTTP(ctx context.Context) (HealthStatus, error) {
	client := &http.Client{
		Timeout: hc.config.Timeout,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", hc.config.URL, nil)
	if err != nil {
		return StatusUnhealthy, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return StatusUnhealthy, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StatusUnhealthy, fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	if hc.config.JQ == "" {
		return StatusHealthy, nil
	}

	var body interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return StatusUnhealthy, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	query, err := gojq.Parse(hc.config.JQ)
	if err != nil {
		return StatusUnhealthy, fmt.Errorf("invalid jq expression: %w", err)
	}

	iter := query.Run(body)
	v, ok := iter.Next()
	if !ok {
		return StatusUnhealthy, fmt.Errorf("jq expression produced no output")
	}
	if err, isErr := v.(error); isErr {
		return StatusUnhealthy, fmt.Errorf("jq evaluation error: %w", err)
	}

	if result, ok := v.(bool); ok && result {
		return StatusHealthy, nil
	}
	return StatusUnhealthy, fmt.Errorf("jq expression evaluated to %v (expected true)", v)
}

func (hc *HealthChecker) checkTCP(ctx context.Context) (HealthStatus, error) {
	d := net.Dialer{Timeout: hc.config.Timeout}
	conn, err := d.DialContext(ctx, "tcp", hc.config.Address)
	if err != nil {
		return StatusUnhealthy, err
	}
	conn.Close()
	return StatusHealthy, nil
}

func (hc *HealthChecker) checkRegex(ctx context.Context) (HealthStatus, error) {
	re, err := regexp.Compile(hc.config.Pattern)
	if err != nil {
		return StatusUnhealthy, err
	}

	lines := hc.buffer.GetLines()
	for _, line := range lines {
		if re.MatchString(line) {
			return StatusHealthy, nil
		}
	}
	return StatusUnhealthy, nil
}

func (hc *HealthChecker) checkDocker(ctx context.Context) (HealthStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.Health.Status}}", hc.config.Container)
	out, err := cmd.Output()
	if err != nil {
		return StatusUnhealthy, fmt.Errorf("docker inspect failed: %w", err)
	}

	status := strings.TrimSpace(string(out))
	if status == "healthy" {
		return StatusHealthy, nil
	}
	return StatusUnhealthy, fmt.Errorf("container health: %s", status)
}
