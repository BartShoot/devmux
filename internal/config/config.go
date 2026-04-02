package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Cwd  string `yaml:"cwd,omitempty"`
	Tabs []Tab  `yaml:"tabs"`
}

type Tab struct {
	Name   string `yaml:"name"`
	Cwd    string `yaml:"cwd,omitempty"`
	Layout string `yaml:"layout,omitempty"` // "vertical" (default), "horizontal", or "split" (first pane left, rest stacked right)
	Panes  []Pane `yaml:"panes"`
}

// ResolveCwd returns the effective working directory for a pane,
// checking pane -> tab -> global cwd in order of precedence.
// Relative paths are resolved against the parent cwd (tab or global).
func (c *Config) ResolveCwd(tab *Tab, pane *Pane) string {
	baseCwd := c.Cwd
	if tab.Cwd != "" {
		if filepath.IsAbs(tab.Cwd) {
			baseCwd = tab.Cwd
		} else if baseCwd != "" {
			baseCwd = filepath.Join(baseCwd, tab.Cwd)
		} else {
			baseCwd = tab.Cwd
		}
	}

	if pane.Cwd != "" {
		if filepath.IsAbs(pane.Cwd) {
			return pane.Cwd
		}
		if baseCwd != "" {
			return filepath.Join(baseCwd, pane.Cwd)
		}
		return pane.Cwd
	}

	return baseCwd
}

type Pane struct {
	Name        string      `yaml:"name"`
	Command     string      `yaml:"command"`
	Cwd         string      `yaml:"cwd,omitempty"`
	HealthCheck HealthCheck `yaml:"health_check,omitempty"`
}

type HealthCheck struct {
	Type     string        `yaml:"type"`
	URL      string        `yaml:"url,omitempty"`
	Address  string        `yaml:"address,omitempty"`
	Pattern  string        `yaml:"pattern,omitempty"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks the config for semantic errors and returns all problems found.
func (c *Config) Validate() error {
	var errs []string

	if len(c.Tabs) == 0 {
		return fmt.Errorf("config validation failed:\n  - no tabs defined")
	}

	paneNames := make(map[string]bool)

	for i, tab := range c.Tabs {
		prefix := fmt.Sprintf("tabs[%d]", i)
		if tab.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: missing name", prefix))
		} else {
			prefix = fmt.Sprintf("tab %q", tab.Name)
		}

		if len(tab.Panes) == 0 {
			errs = append(errs, fmt.Sprintf("%s: no panes defined", prefix))
			continue
		}

		for j, pane := range tab.Panes {
			pprefix := fmt.Sprintf("%s.panes[%d]", prefix, j)
			if pane.Name == "" {
				errs = append(errs, fmt.Sprintf("%s: missing name", pprefix))
			} else {
				pprefix = fmt.Sprintf("%s pane %q", prefix, pane.Name)
				if paneNames[pane.Name] {
					errs = append(errs, fmt.Sprintf("%s: duplicate pane name", pprefix))
				}
				paneNames[pane.Name] = true
			}

			if pane.Command == "" {
				errs = append(errs, fmt.Sprintf("%s: missing command", pprefix))
			}

			if err := validateHealthCheck(pane.HealthCheck, pprefix); err != "" {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateHealthCheck(hc HealthCheck, prefix string) string {
	if hc.Type == "" {
		return ""
	}

	switch hc.Type {
	case "http":
		if hc.URL == "" {
			return fmt.Sprintf("%s: health check type %q requires url", prefix, hc.Type)
		}
	case "tcp":
		if hc.Address == "" {
			return fmt.Sprintf("%s: health check type %q requires address", prefix, hc.Type)
		}
	case "regex":
		if hc.Pattern == "" {
			return fmt.Sprintf("%s: health check type %q requires pattern", prefix, hc.Type)
		}
	default:
		return fmt.Sprintf("%s: unknown health check type %q (must be http, tcp, or regex)", prefix, hc.Type)
	}

	if hc.Interval <= 0 {
		return fmt.Sprintf("%s: health check interval must be positive", prefix)
	}
	if hc.Timeout <= 0 {
		return fmt.Sprintf("%s: health check timeout must be positive", prefix)
	}

	return ""
}
