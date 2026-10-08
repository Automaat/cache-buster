package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	// Protected lists paths auto never deletes and status reports for a human.
	// User entries are added to the built-in ones, never replace them.
	Protected []string `mapstructure:"protected" yaml:"protected,omitempty"`
}

// DefaultProtected returns the built-in protected paths. The docker volumes
// path is the Linux location; Docker Desktop keeps volumes in a VM image
// that auto never prunes.
func DefaultProtected() []string {
	return []string{"~/Downloads", "~/.local/share/opencode", "/var/lib/docker/volumes"}
}

// MergeProtected returns the built-in protected paths followed by the extra
// ones, without duplicates.
func MergeProtected(extra []string) []string {
	out := DefaultProtected()
	for _, p := range extra {
		p = strings.TrimSpace(p)
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
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
	// SkipPrefixes lists entry-name prefixes that whole-entry providers never evict.
	SkipPrefixes []string `mapstructure:"skip_prefixes" yaml:"skip_prefixes,omitempty"`
}

// TypeDirPattern is the provider type that removes whole stale directories matching a glob.
const TypeDirPattern = "dir-pattern"

// Validate checks config for required fields.
func (c *Config) Validate() error {
	if err := c.Auto.Validate(); err != nil {
		return fmt.Errorf("auto: %w", err)
	}
	if err := c.validateProtected(); err != nil {
		return err
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

// validateProtected rejects entries that are malformed or so broad they would
// protect everything: relative paths, whitespace, empty, "." or ".." elements,
// the filesystem root, home and any parent of home.
func (c *Config) validateProtected() error {
	home, _ := os.UserHomeDir()
	for _, path := range c.Protected {
		if err := validateProtectedEntry(path, home); err != nil {
			return fmt.Errorf("protected: %w", err)
		}
	}
	return nil
}

func validateProtectedEntry(path, home string) error {
	if path != strings.TrimSpace(path) {
		return fmt.Errorf("%q has leading or trailing whitespace", path)
	}
	rest, tilde := strings.CutPrefix(path, "~/")
	if !tilde {
		var abs bool
		rest, abs = strings.CutPrefix(path, "/")
		if !abs {
			return fmt.Errorf("paths must be absolute or start with ~/, got %q", path)
		}
	}
	if rest == "" {
		return fmt.Errorf("%q is too broad, name a specific directory", path)
	}
	for elem := range strings.SplitSeq(rest, "/") {
		if elem == "" || elem == "." || elem == ".." {
			return fmt.Errorf("%q has an empty, '.' or '..' path element", path)
		}
	}

	if strings.ContainsAny(path, "*?[") {
		return fmt.Errorf("%q contains glob characters, protected entries are literal paths", path)
	}
	if !tilde && !strings.Contains(strings.TrimPrefix(stripDataAlias(path), "/"), "/") {
		return fmt.Errorf("%q is a top-level system directory, name a deeper path", path)
	}

	expanded := "/" + rest
	if tilde {
		if home == "" {
			return nil
		}
		expanded = filepath.Join(home, rest)
	}
	homes := []string{filepath.Clean(home)}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		homes = append(homes, resolved)
	}
	candidates := []string{filepath.Clean(expanded)}
	if resolved, err := filepath.EvalSymlinks(expanded); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, c := range slices.Clone(candidates) {
		candidates = append(candidates, stripDataAlias(c))
	}
	for _, h := range slices.Clone(homes) {
		homes = append(homes, stripDataAlias(h))
	}
	for _, cand := range candidates {
		if cand == "/" {
			return fmt.Errorf("%q is the filesystem root, name a specific directory", path)
		}
		for _, h := range homes {
			if h == "." || h == "" {
				continue
			}
			if strings.EqualFold(cand, h) || strings.HasPrefix(strings.ToLower(h), strings.ToLower(cand)+"/") {
				return fmt.Errorf("%q is home or a parent of home, name a specific directory", path)
			}
		}
	}
	return nil
}

// dataVolumeAlias is the macOS firmlink that spells the root filesystem
// again under the user data volume.
const dataVolumeAlias = "/System/Volumes/Data"

// stripDataAlias returns p without the data volume firmlink prefix.
func stripDataAlias(p string) string {
	if len(p) < len(dataVolumeAlias) || !strings.EqualFold(p[:len(dataVolumeAlias)], dataVolumeAlias) {
		return p
	}
	rest := p[len(dataVolumeAlias):]
	if rest == "" {
		return "/"
	}
	if strings.HasPrefix(rest, "/") {
		return rest
	}
	return p
}
