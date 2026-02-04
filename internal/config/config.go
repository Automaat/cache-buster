package config

import (
	"fmt"
	"sort"
)

type Config struct {
	Version   string              `mapstructure:"version" yaml:"version"`
	Providers map[string]Provider `mapstructure:"providers" yaml:"providers"`
}

type Provider struct {
	Enabled  bool     `mapstructure:"enabled" yaml:"enabled"`
	Paths    []string `mapstructure:"paths" yaml:"paths"`
	MaxSize  string   `mapstructure:"max_size" yaml:"max_size"`
	CleanCmd string   `mapstructure:"clean_cmd" yaml:"clean_cmd,omitempty"`
}

func (c *Config) Validate() error {
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}

	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	for name, p := range c.Providers {
		if len(p.Paths) == 0 {
			return fmt.Errorf("provider %q: at least one path is required", name)
		}
		if p.MaxSize == "" {
			return fmt.Errorf("provider %q: max_size is required", name)
		}
	}

	return nil
}

func (c *Config) GetProvider(name string) (Provider, bool) {
	p, ok := c.Providers[name]
	return p, ok
}

func (c *Config) EnabledProviders() []string {
	var enabled []string
	for name, p := range c.Providers {
		if p.Enabled {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)
	return enabled
}
