package config

import (
	"fmt"
	"sort"
)

// Config holds cache-buster configuration.
type Config struct {
	Providers map[string]Provider `mapstructure:"providers" yaml:"providers"`
	Version   string              `mapstructure:"version" yaml:"version"`
}

// Provider defines a cache provider's settings.
type Provider struct {
	MaxSize  string   `mapstructure:"max_size" yaml:"max_size"`
	MaxAge   string   `mapstructure:"max_age" yaml:"max_age,omitempty"`
	CleanCmd string   `mapstructure:"clean_cmd" yaml:"clean_cmd,omitempty"`
	Paths    []string `mapstructure:"paths" yaml:"paths"`
	Enabled  bool     `mapstructure:"enabled" yaml:"enabled"`
}

// Validate checks config for required fields.
func (c *Config) Validate() error {
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

// GetProvider returns provider by name.
func (c *Config) GetProvider(name string) (Provider, bool) {
	p, ok := c.Providers[name]
	return p, ok
}

// EnabledProviders returns sorted list of enabled provider names with existing paths.
func (c *Config) EnabledProviders() []string {
	var enabled []string
	for name, p := range c.Providers {
		if p.Enabled && PathsExist(p.Paths) {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)
	return enabled
}

// AllEnabledProviders returns all enabled providers regardless of path existence.
func (c *Config) AllEnabledProviders() []string {
	var enabled []string
	for name, p := range c.Providers {
		if p.Enabled {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)
	return enabled
}
