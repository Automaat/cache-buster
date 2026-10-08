package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Automaat/cache-buster/pkg/size"
)

// Config holds cache-buster configuration.
type Config struct {
	Providers map[string]Provider `mapstructure:"providers" yaml:"providers"`
	Version   string              `mapstructure:"version" yaml:"version"`
	Auto      Auto                `mapstructure:"auto" yaml:"auto"`
}

// Auto configures the unattended auto command and its launchd agent.
type Auto struct {
	// Interval is how often the launchd agent runs auto (default 30m).
	Interval string `mapstructure:"interval" yaml:"interval"`
	// MinFree is the free-space floor; below it auto trims every enabled provider (default 30G).
	MinFree string `mapstructure:"min_free" yaml:"min_free"`
	// MinFreePct is the free-space floor as a percentage of the volume (default 15).
	MinFreePct float64 `mapstructure:"min_free_pct" yaml:"min_free_pct"`
}

// Auto defaults.
const (
	DefaultAutoInterval   = "30m"
	DefaultAutoMinFree    = "30G"
	DefaultAutoMinFreePct = 15.0
)

// MinAutoInterval is the shortest accepted agent interval.
const MinAutoInterval = time.Minute

// DefaultAuto returns the auto settings used when the config omits them.
func DefaultAuto() Auto {
	return Auto{Interval: DefaultAutoInterval, MinFree: DefaultAutoMinFree, MinFreePct: DefaultAutoMinFreePct}
}

// Resolved fills blank interval and min_free with their defaults.
func (a Auto) Resolved() Auto {
	if strings.TrimSpace(a.Interval) == "" {
		a.Interval = DefaultAutoInterval
	}
	if strings.TrimSpace(a.MinFree) == "" {
		a.MinFree = DefaultAutoMinFree
	}
	return a
}

// IntervalDuration parses Interval.
func (a Auto) IntervalDuration() (time.Duration, error) {
	a = a.Resolved()
	d, err := ParseDuration(a.Interval)
	if err != nil {
		return 0, fmt.Errorf("interval: %w", err)
	}
	if d < MinAutoInterval {
		return 0, fmt.Errorf("interval must be at least %s, got %q", MinAutoInterval, a.Interval)
	}
	return d, nil
}

// MinFreeBytes parses MinFree.
func (a Auto) MinFreeBytes() (int64, error) {
	a = a.Resolved()
	b, err := size.ParseSize(a.MinFree)
	if err != nil {
		return 0, fmt.Errorf("min_free: %w", err)
	}
	if b < 0 {
		return 0, fmt.Errorf("min_free must not be negative, got %q", a.MinFree)
	}
	return b, nil
}

// Validate checks the auto settings.
func (a Auto) Validate() error {
	if _, err := a.IntervalDuration(); err != nil {
		return err
	}
	if _, err := a.MinFreeBytes(); err != nil {
		return err
	}
	if a.MinFreePct < 0 || a.MinFreePct > 100 {
		return fmt.Errorf("min_free_pct must be between 0 and 100, got %v", a.MinFreePct)
	}
	return nil
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
	if err := c.Auto.Validate(); err != nil {
		return fmt.Errorf("auto: %w", err)
	}
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
			if strings.TrimSpace(p.CleanTimeout) == "" {
				return fmt.Errorf("provider %q: clean_timeout must not be blank", name)
			}
			if p.CleanCmd == "" {
				return fmt.Errorf("provider %q: clean_timeout requires clean_cmd", name)
			}
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
