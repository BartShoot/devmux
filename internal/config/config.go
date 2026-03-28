package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Tabs []Tab `yaml:"tabs"`
}

type Tab struct {
	Name  string `yaml:"name"`
	Panes []Pane `yaml:"panes"`
}

type Pane struct {
	Name        string      `yaml:"name"`
	Command     string      `yaml:"command"`
	HealthCheck HealthCheck `yaml:"health_check"`
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
