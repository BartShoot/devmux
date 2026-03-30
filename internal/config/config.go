package config

import (
	"fmt"
	"os"
	"path/filepath"
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

	return &cfg, nil
}
