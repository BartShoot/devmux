package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cmds(s ...string) []CommandEntry {
	var entries []CommandEntry
	for _, c := range s {
		entries = append(entries, CommandEntry{Command: c})
	}
	return entries
}

func TestLoad_ValidConfig(t *testing.T) {
	content := `
tabs:
  - name: "Test"
    panes:
      - name: "App"
        commands:
          - "ls"
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
	if len(pane.Commands) != 1 || pane.Commands[0].Command != "ls" {
		t.Errorf("Expected commands [ls], got %v", pane.Commands)
	}
	if pane.HealthCheck.Interval != 1*time.Second {
		t.Errorf("Expected interval 1s, got %v", pane.HealthCheck.Interval)
	}
}

func TestLoad_LabeledCommands(t *testing.T) {
	content := `
tabs:
  - name: "Test"
    panes:
      - name: "App"
        commands:
          - normal: "go run ./cmd/server"
          - debug: "go run ./cmd/server -debug"
          - "plain command"
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

	pane := cfg.Tabs[0].Panes[0]
	if len(pane.Commands) != 3 {
		t.Fatalf("Expected 3 commands, got %d", len(pane.Commands))
	}
	if pane.Commands[0].Label != "normal" || pane.Commands[0].Command != "go run ./cmd/server" {
		t.Errorf("Command 0: got %+v", pane.Commands[0])
	}
	if pane.Commands[1].Label != "debug" || pane.Commands[1].Command != "go run ./cmd/server -debug" {
		t.Errorf("Command 1: got %+v", pane.Commands[1])
	}
	if pane.Commands[2].Label != "" || pane.Commands[2].Command != "plain command" {
		t.Errorf("Command 2: got %+v", pane.Commands[2])
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

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsgs []string // substrings expected in error
	}{
		{
			name:    "no tabs",
			config:  Config{},
			wantErr: true,
			errMsgs: []string{"no tabs defined"},
		},
		{
			name: "tab without name",
			config: Config{Tabs: []Tab{
				{Panes: []Pane{{Name: "p", Commands: cmds("ls")}}},
			}},
			wantErr: true,
			errMsgs: []string{"missing name"},
		},
		{
			name: "tab without panes",
			config: Config{Tabs: []Tab{
				{Name: "t"},
			}},
			wantErr: true,
			errMsgs: []string{"no panes defined"},
		},
		{
			name: "pane without name",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Commands: cmds("ls")}}},
			}},
			wantErr: true,
			errMsgs: []string{"missing name"},
		},
		{
			name: "pane without commands",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p"}}},
			}},
			wantErr: true,
			errMsgs: []string{"missing commands"},
		},
		{
			name: "duplicate pane names",
			config: Config{Tabs: []Tab{
				{Name: "t1", Panes: []Pane{{Name: "p", Commands: cmds("ls")}}},
				{Name: "t2", Panes: []Pane{{Name: "p", Commands: cmds("ls")}}},
			}},
			wantErr: true,
			errMsgs: []string{"duplicate pane name"},
		},
		{
			name: "unknown health check type",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "magic"}}}},
			}},
			wantErr: true,
			errMsgs: []string{"unknown health check type"},
		},
		{
			name: "http health check missing url",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "http", Interval: time.Second, Timeout: time.Second}}}},
			}},
			wantErr: true,
			errMsgs: []string{"requires url"},
		},
		{
			name: "tcp health check missing address",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "tcp", Interval: time.Second, Timeout: time.Second}}}},
			}},
			wantErr: true,
			errMsgs: []string{"requires address"},
		},
		{
			name: "regex health check missing pattern",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "regex", Interval: time.Second, Timeout: time.Second}}}},
			}},
			wantErr: true,
			errMsgs: []string{"requires pattern"},
		},
		{
			name: "health check missing interval",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "http", URL: "http://localhost", Timeout: time.Second}}}},
			}},
			wantErr: true,
			errMsgs: []string{"interval must be positive"},
		},
		{
			name: "multiple errors reported at once",
			config: Config{Tabs: []Tab{
				{Panes: []Pane{{}, {Name: "p"}}},
			}},
			wantErr: true,
			errMsgs: []string{"missing name", "missing commands"},
		},
		{
			name: "valid minimal config",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls")}}},
			}},
			wantErr: false,
		},
		{
			name: "valid config with health checks",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{
					Type: "http", URL: "http://localhost", Interval: time.Second, Timeout: 5 * time.Second,
				}}}},
			}},
			wantErr: false,
		},
		{
			name: "pane without health check is valid",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls")}}},
			}},
			wantErr: false,
		},
		{
			name: "docker health check missing container",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{Type: "docker", Interval: time.Second, Timeout: time.Second}}}},
			}},
			wantErr: true,
			errMsgs: []string{"requires container"},
		},
		{
			name: "valid docker health check",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{
					Type: "docker", Container: "my_app", Interval: time.Second, Timeout: 5 * time.Second,
				}}}},
			}},
			wantErr: false,
		},
		{
			name: "valid http health check with jq",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{
					Type: "http", URL: "http://localhost", JQ: `.status == "OK"`, Interval: time.Second, Timeout: 5 * time.Second,
				}}}},
			}},
			wantErr: false,
		},
		{
			name: "jq on non-http type is invalid",
			config: Config{Tabs: []Tab{
				{Name: "t", Panes: []Pane{{Name: "p", Commands: cmds("ls"), HealthCheck: HealthCheck{
					Type: "tcp", Address: "localhost:8080", JQ: ".status", Interval: time.Second, Timeout: 5 * time.Second,
				}}}},
			}},
			wantErr: true,
			errMsgs: []string{"jq expression is only supported with health check type \"http\""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				for _, msg := range tt.errMsgs {
					if !strings.Contains(err.Error(), msg) {
						t.Errorf("error %q should contain %q", err.Error(), msg)
					}
				}
			}
		})
	}
}
