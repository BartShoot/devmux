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

// CommandEntry represents a single command preset with an optional label.
type CommandEntry struct {
	Label   string `yaml:"-"`
	Command string `yaml:"-"`
}

// MarshalYAML encodes a CommandEntry as either a plain string or a single-key map.
func (c CommandEntry) MarshalYAML() (interface{}, error) {
	if c.Label == "" {
		return c.Command, nil
	}
	return map[string]string{c.Label: c.Command}, nil
}

type Pane struct {
	Name        string         `yaml:"name"`
	Commands    []CommandEntry `yaml:"commands"`
	Cwd         string         `yaml:"cwd,omitempty"`
	HealthCheck HealthCheck    `yaml:"health_check,omitempty"`
}

// UnmarshalYAML handles the mixed commands list: plain strings and single-key maps.
func (p *Pane) UnmarshalYAML(value *yaml.Node) error {
	// Decode into a raw struct to get all other fields
	var raw struct {
		Name        string      `yaml:"name"`
		Cwd         string      `yaml:"cwd,omitempty"`
		HealthCheck HealthCheck `yaml:"health_check,omitempty"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	p.Name = raw.Name
	p.Cwd = raw.Cwd
	p.HealthCheck = raw.HealthCheck

	// Find the "commands" key in the mapping
	for i := 0; i < len(value.Content)-1; i += 2 {
		if value.Content[i].Value == "commands" {
			seqNode := value.Content[i+1]
			if seqNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("commands must be a list")
			}
			for _, item := range seqNode.Content {
				switch item.Kind {
				case yaml.ScalarNode:
					// Plain string: "gradle bootrun"
					p.Commands = append(p.Commands, CommandEntry{Command: item.Value})
				case yaml.MappingNode:
					// Single-key map: debug: "mvnDebug exec:java"
					if len(item.Content) != 2 {
						return fmt.Errorf("command map entry must have exactly one key")
					}
					p.Commands = append(p.Commands, CommandEntry{
						Label:   item.Content[0].Value,
						Command: item.Content[1].Value,
					})
				default:
					return fmt.Errorf("unexpected node type in commands list")
				}
			}
			break
		}
	}

	return nil
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

			if len(pane.Commands) == 0 {
				errs = append(errs, fmt.Sprintf("%s: missing commands", pprefix))
			}
			for k, cmd := range pane.Commands {
				if cmd.Command == "" {
					errs = append(errs, fmt.Sprintf("%s commands[%d]: empty command", pprefix, k))
				}
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
