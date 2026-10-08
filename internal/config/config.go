package config

import (
	"fmt"
	"sort"
	"strings"
)

// Config holds cache-buster configuration.
type Config struct {
	Providers map[string]Provider `mapstructure:"providers" yaml:"providers"`
	Version   string              `mapstructure:"version" yaml:"version"`
}

// Provider defines a cache provider's settings.
type Provider struct {
	MaxSize  string `mapstructure:"max_size" yaml:"max_size"`
	MaxAge   string `mapstructure:"max_age" yaml:"max_age,omitempty"`
	CleanCmd string `mapstructure:"clean_cmd" yaml:"clean_cmd,omitempty"`
	// CleanTimeout bounds the clean command (default 2m); a hung command is cancelled.
	CleanTimeout string   `mapstructure:"clean_timeout" yaml:"clean_timeout,omitempty"`
	Paths        []string `mapstructure:"paths" yaml:"paths"`
	Enabled      bool     `mapstructure:"enabled" yaml:"enabled"`

	// Type selects a special provider implementation (see TypeDirPattern).
	Type string `mapstructure:"type" yaml:"type,omitempty"`
	// MinIdle is the minimum idle time before a directory-pattern match may be removed.
	MinIdle string `mapstructure:"min_idle" yaml:"min_idle,omitempty"`
	// SkipIfOpen guards directory-pattern matches that have open files (default true).
	SkipIfOpen *bool `mapstructure:"skip_if_open" yaml:"skip_if_open,omitempty"`
	// SkipIfGitWorktree guards directory-pattern matches containing .git (default true).
	SkipIfGitWorktree *bool `mapstructure:"skip_if_git_worktree" yaml:"skip_if_git_worktree,omitempty"`
}

// TypeDirPattern is the provider type that removes whole stale directories matching a glob.
const TypeDirPattern = "dir-pattern"

// Validate checks config for required fields.
func (c *Config) Validate() error {
	for name := range c.Providers {
		p := c.Providers[name]
		if strings.Contains(name, ".") {
			return fmt.Errorf("provider %q: must not contain '.' (reserved as Viper key delimiter)", name)
		}
		if p.Type != "" && p.Type != TypeDirPattern {
			return fmt.Errorf("provider %q: unknown type %q", name, p.Type)
		}
		if p.Type == TypeDirPattern {
			for _, path := range p.Paths {
				if !strings.ContainsAny(path, "*?[") {
					return fmt.Errorf("provider %q: %s paths must contain a glob (*, ? or [), got %q",
						name, TypeDirPattern, path)
				}
				if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~/") {
					return fmt.Errorf("provider %q: %s paths must be absolute or start with ~/, got %q",
						name, TypeDirPattern, path)
				}
			}
		}
		if p.CleanTimeout != "" {
			d, err := ParseDuration(p.CleanTimeout)
			if err != nil {
				return fmt.Errorf("provider %q: clean_timeout: %w", name, err)
			}
			if d <= 0 {
				return fmt.Errorf("provider %q: clean_timeout must be positive, got %q", name, p.CleanTimeout)
			}
		}
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
	for name := range c.Providers {
		p := c.Providers[name]
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
	for name := range c.Providers {
		if c.Providers[name].Enabled {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)
	return enabled
}
