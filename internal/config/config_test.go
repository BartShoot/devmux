package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `
tabs:
  - name: "Test"
    panes:
      - name: "App"
        command: "ls"
        health_check:
          type: "http"
          url: "http://localhost:8080"
          interval: 1s
          timeout: 2s
`
	tmpfile, err := os.CreateTemp("", "devmux-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Tabs) != 1 {
		t.Errorf("Expected 1 tab, got %d", len(cfg.Tabs))
	}

	tab := cfg.Tabs[0]
	if tab.Name != "Test" {
		t.Errorf("Expected tab name 'Test', got '%s'", tab.Name)
	}

	pane := tab.Panes[0]
	if pane.HealthCheck.Interval != 1*time.Second {
		t.Errorf("Expected interval 1s, got %v", pane.HealthCheck.Interval)
	}
}
