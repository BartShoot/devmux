package config

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestResolveCwd(t *testing.T) {
	isWindows := runtime.GOOS == "windows"
	globalCwd := "/global"
	if isWindows {
		globalCwd = `C:\global`
	}

	cfg := &Config{
		Cwd: globalCwd,
	}

	tab := Tab{
		Name: "Tab1",
		Cwd:  "tabdir",
	}

	pane := Pane{
		Name: "Pane1",
		Cwd:  "panedir",
	}

	// 1. All relative: /global/tabdir/panedir
	cwd := cfg.ResolveCwd(&tab, &pane)
	expected := filepath.Join(globalCwd, "tabdir", "panedir")
	if filepath.ToSlash(cwd) != filepath.ToSlash(expected) {
		t.Errorf("1. expected %s, got %s", expected, cwd)
	}

	// 2. Absolute tab cwd: /abs-tab/panedir
	absTab := "/abs-tab"
	if isWindows {
		absTab = `C:\abs-tab`
	}
	tab.Cwd = absTab
	cwd = cfg.ResolveCwd(&tab, &pane)
	expected = filepath.Join(absTab, "panedir")
	if filepath.ToSlash(cwd) != filepath.ToSlash(expected) {
		t.Errorf("2. expected %s, got %s", expected, cwd)
	}

	// 3. Absolute pane cwd: /abs-pane
	absPane := "/abs-pane"
	if isWindows {
		absPane = `C:\abs-pane`
	}
	pane.Cwd = absPane
	cwd = cfg.ResolveCwd(&tab, &pane)
	expected = absPane
	if filepath.ToSlash(cwd) != filepath.ToSlash(expected) {
		t.Errorf("3. expected %s, got %s", expected, cwd)
	}

	// 4. No tab cwd, relative pane: /global/panedir
	tab.Cwd = ""
	pane.Cwd = "panedir"
	cwd = cfg.ResolveCwd(&tab, &pane)
	expected = filepath.Join(globalCwd, "panedir")
	if filepath.ToSlash(cwd) != filepath.ToSlash(expected) {
		t.Errorf("4. expected %s, got %s", expected, cwd)
	}

	// 5. No global cwd, relative everything: tabdir/panedir
	cfg.Cwd = ""
	tab.Cwd = "tabdir"
	pane.Cwd = "panedir"
	cwd = cfg.ResolveCwd(&tab, &pane)
	expected = filepath.Join("tabdir", "panedir")
	if filepath.ToSlash(cwd) != filepath.ToSlash(expected) {
		t.Errorf("5. expected %s, got %s", expected, cwd)
	}
}
