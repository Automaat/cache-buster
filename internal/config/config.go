package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/smykla-skalski/bilgie/pkg/size"
)

// Config holds bilgie configuration.
type Config struct {
	Providers map[string]Provider `mapstructure:"providers" yaml:"providers"`
	Version   string              `mapstructure:"version" yaml:"version"`
	Auto      Auto                `mapstructure:"auto" yaml:"auto"`
	// Protected lists paths auto never deletes and status reports for a human.
	// User entries are added to the built-in ones, never replace them.
	Protected []string `mapstructure:"protected" yaml:"protected,omitempty"`

	goos string
}

// Applies reports whether the named provider applies on the OS this config
// was built for. Providers stay in the file so a config shared between OSes
// keeps entries that only apply elsewhere.
func (c *Config) Applies(name string) bool {
	goos := c.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	return AppliesOn(name, goos)
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
	Paths        []string `mapstructure:"paths" yaml:"paths,omitempty"`
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

	// MaxDepth bounds how deep project-artifacts looks below each root (default 4).
	MaxDepth int `mapstructure:"max_depth" yaml:"max_depth,omitempty"`
	// ScanBudget bounds project discovery per pass for project-artifacts (default 10s).
	ScanBudget string `mapstructure:"scan_budget" yaml:"scan_budget,omitempty"`
	// SkipIfDirty skips projects with uncommitted changes for project-artifacts (default true).
	SkipIfDirty *bool `mapstructure:"skip_if_dirty" yaml:"skip_if_dirty,omitempty"`
	// Rust, Node and Python switch the per-kind project-artifacts detectors
	// (rust and node default to true, python to false).
	Rust   *bool `mapstructure:"rust" yaml:"rust,omitempty"`
	Node   *bool `mapstructure:"node" yaml:"node,omitempty"`
	Python *bool `mapstructure:"python" yaml:"python,omitempty"`
}

// TypeDirPattern is the provider type that removes whole stale directories matching a glob.
const TypeDirPattern = "dir-pattern"

// TypeProjectArtifacts is the provider type that removes whole build artifact
// directories (Rust target, node_modules, Python venvs) of idle projects found
// below the roots in paths.
const TypeProjectArtifacts = "project-artifacts"

// MaxProjectDepth is the deepest max_depth accepted.
const MaxProjectDepth = 16

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
		if p.Type != "" && p.Type != TypeDirPattern && p.Type != TypeProjectArtifacts {
			return fmt.Errorf("provider %q: unknown type %q", name, p.Type)
		}
		if p.Type == TypeDirPattern {
			for _, path := range p.Paths {
				if !strings.ContainsAny(path, "*?[") {
					return fmt.Errorf("provider %q: %s paths must contain a glob (*, ? or [), got %q",
						name, TypeDirPattern, path)
				}
				if !IsAbsPortable(path) && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
					return fmt.Errorf("provider %q: %s paths must be absolute or start with ~/, got %q",
						name, TypeDirPattern, path)
				}
			}
		}
		if p.Type == TypeProjectArtifacts {
			if err := p.validateProjectArtifacts(); err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
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
		if slices.ContainsFunc(p.Paths, func(path string) bool { return strings.TrimSpace(path) == "" }) {
			return fmt.Errorf("provider %q: paths must not be blank", name)
		}
		if p.MaxSize == "" {
			return fmt.Errorf("provider %q: max_size is required", name)
		}
	}
	return nil
}

// validateProjectArtifacts checks the project-artifacts settings.
func (p Provider) validateProjectArtifacts() error {
	for _, path := range p.Paths {
		if !IsAbsPortable(path) && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
			return fmt.Errorf("%s paths must be absolute or start with ~/, got %q", TypeProjectArtifacts, path)
		}
		if strings.ContainsAny(path, "*?[") {
			return fmt.Errorf("%s paths are literal roots, globs are not allowed: %q", TypeProjectArtifacts, path)
		}
	}
	if p.MaxDepth < 0 || p.MaxDepth > MaxProjectDepth {
		return fmt.Errorf("max_depth must be between 0 and %d, got %d", MaxProjectDepth, p.MaxDepth)
	}
	for field, value := range map[string]string{"min_idle": p.MinIdle, "scan_budget": p.ScanBudget} {
		if value == "" {
			continue
		}
		d, err := ParseDuration(value)
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		if d <= 0 {
			return fmt.Errorf("%s must be positive, got %q", field, value)
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
		if p.Enabled && c.Applies(name) && PathsExist(p.Paths) {
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
		if c.Providers[name].Enabled && c.Applies(name) {
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
	norm := strings.ReplaceAll(path, `\`, "/")
	rest, tilde := strings.CutPrefix(norm, "~/")
	drive := ""
	if !tilde {
		var abs bool
		switch {
		case hasDrive(norm) && len(norm) > 2 && norm[2] == '/':
			drive, rest, abs = norm[:2], norm[3:], true
		default:
			rest, abs = strings.CutPrefix(norm, "/")
		}
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
	topLevel := rest
	if drive == "" {
		topLevel = strings.TrimPrefix(stripDataAlias(norm), "/")
	}
	if !tilde && !strings.Contains(topLevel, "/") {
		return fmt.Errorf("%q is a top-level system directory, name a deeper path", path)
	}

	expanded := "/" + rest
	switch {
	case tilde:
		if home == "" {
			return nil
		}
		expanded = filepath.Join(home, rest)
	case drive != "":
		expanded = drive + "/" + rest
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
		cand = filepath.ToSlash(cand)
		if cand == "/" {
			return fmt.Errorf("%q is the filesystem root, name a specific directory", path)
		}
		for _, h := range homes {
			h = filepath.ToSlash(h)
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

// stripDataAlias returns p without the data volume firmlink prefix, using
// slash separators when it strips.
func stripDataAlias(p string) string {
	slashed := filepath.ToSlash(p)
	if len(slashed) < len(dataVolumeAlias) || !strings.EqualFold(slashed[:len(dataVolumeAlias)], dataVolumeAlias) {
		return p
	}
	rest := slashed[len(dataVolumeAlias):]
	if rest == "" {
		return "/"
	}
	if strings.HasPrefix(rest, "/") {
		return rest
	}
	return p
}
