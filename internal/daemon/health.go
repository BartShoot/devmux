package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"

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

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusHealthy, nil
	}
	return StatusUnhealthy, fmt.Errorf("HTTP status: %d", resp.StatusCode)
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
