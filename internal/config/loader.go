package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/viper"
)

// Loader handles config file operations.
type Loader struct {
	v            *viper.Viper
	configPath   string // override for testing, empty uses Path()
	skipDefaults bool   // skip merging with defaults (for test isolation)
	platform     Platform
}

// NewLoader creates a new config loader.
func NewLoader() *Loader {
	return &Loader{v: viper.New(), platform: CurrentPlatform()}
}

// SetPlatform overrides the OS and environment the defaults are built for.
func (l *Loader) SetPlatform(p Platform) {
	l.platform = p
}

// SetConfigPath overrides config path (for testing).
func (l *Loader) SetConfigPath(path string) {
	l.configPath = path
}

// SkipDefaults disables merging with defaults (for test isolation).
func (l *Loader) SkipDefaults() {
	l.skipDefaults = true
}

func (l *Loader) path() (string, error) {
	if l.configPath != "" {
		return l.configPath, nil
	}
	return Path()
}

// Load reads config from disk and merges with defaults.
func (l *Loader) Load() (*Config, error) {
	var cfg *Config
	if l.skipDefaults {
		cfg = &Config{
			Version: "1", Providers: make(map[string]Provider), Auto: DefaultAuto(),
			Protected: DefaultProtected(), goos: l.platform.OS,
		}
	} else {
		cfg = DefaultConfigFor(l.platform)
	}

	configPath, err := l.path()
	if err != nil {
		return nil, err
	}

	l.v.SetConfigFile(configPath)
	l.v.SetConfigType("yaml")

	if err := l.v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var userCfg Config
	if err := l.v.Unmarshal(&userCfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	l.mergeAuto(cfg, &userCfg)
	// Union, never replace: removing a built-in entry must not unprotect it.
	cfg.Protected = MergeProtected(append(slices.Clone(cfg.Protected), userCfg.Protected...))
	if err := cfg.validateProtected(); err != nil {
		return nil, err
	}

	// Merge user overrides on top of defaults, field by field.
	for name := range userCfg.Providers {
		userP := userCfg.Providers[name]
		defaultP, hasDefault := cfg.Providers[name]
		if !hasDefault {
			// New provider not in defaults: use as-is; do not auto-enable when `enabled` is omitted.
			cfg.Providers[name] = userP
			continue
		}
		merged := defaultP
		if l.v.IsSet("providers." + name + ".max_size") {
			merged.MaxSize = userP.MaxSize
		}
		if l.v.IsSet("providers." + name + ".max_age") {
			merged.MaxAge = userP.MaxAge
		}
		if l.v.IsSet("providers." + name + ".clean_cmd") {
			merged.CleanCmd = userP.CleanCmd
		}
		if l.v.IsSet("providers." + name + ".clean_timeout") {
			merged.CleanTimeout = userP.CleanTimeout
		}
		if l.v.IsSet("providers."+name+".paths") && !l.isForeignDefault(name, userP.Paths) {
			merged.Paths = userP.Paths
		}
		if l.v.IsSet("providers." + name + ".enabled") {
			merged.Enabled = userP.Enabled
		}
		if l.v.IsSet("providers." + name + ".type") {
			merged.Type = userP.Type
		}
		if l.v.IsSet("providers." + name + ".min_idle") {
			merged.MinIdle = userP.MinIdle
		}
		if l.v.IsSet("providers." + name + ".skip_if_open") {
			merged.SkipIfOpen = userP.SkipIfOpen
		}
		if l.v.IsSet("providers." + name + ".skip_if_git_worktree") {
			merged.SkipIfGitWorktree = userP.SkipIfGitWorktree
		}
		// Union, never replace: an empty user list must not strip protections.
		if l.v.IsSet("providers." + name + ".skip_prefixes") {
			merged.SkipPrefixes = append(slices.Clone(defaultP.SkipPrefixes), userP.SkipPrefixes...)
		}
		cfg.Providers[name] = merged
	}

	return cfg, nil
}

// mergeAuto applies the user's auto settings over the defaults, field by field.
func (l *Loader) mergeAuto(cfg, userCfg *Config) {
	if l.v.IsSet("auto.interval") {
		cfg.Auto.Interval = userCfg.Auto.Interval
	}
	if l.v.IsSet("auto.min_free") {
		cfg.Auto.MinFree = userCfg.Auto.MinFree
	}
	if l.v.IsSet("auto.min_free_pct") {
		cfg.Auto.MinFreePct = userCfg.Auto.MinFreePct
	}
}

// LoadOrCreate loads config (always merges with defaults). Returns (config, created, error).
//
// Deprecated: Use Load() instead. The created return value is always false.
func (l *Loader) LoadOrCreate() (*Config, bool, error) {
	cfg, err := l.Load()
	return cfg, false, err
}

// Save writes config to disk.
func (l *Loader) Save(cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	configPath, err := l.path()
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	for key, value := range map[string]any{
		"version":   cfg.Version,
		"providers": l.portableProviders(cfg.Providers),
	} {
		l.v.Set(key, value)
	}
	// Defaults stay out of the file so a later default change still applies.
	if cfg.Auto != DefaultAuto() {
		l.v.Set("auto", cfg.Auto)
	}

	// Built-in entries stay out of the file so a later default change still applies.
	extras := slices.DeleteFunc(slices.Clone(cfg.Protected), func(p string) bool {
		return slices.Contains(DefaultProtected(), p)
	})
	if len(extras) > 0 || l.v.IsSet("protected") {
		l.v.Set("protected", extras)
	}

	return l.v.WriteConfigAs(configPath)
}

// InitDefault creates default config if missing. Returns true if created.
func (l *Loader) InitDefault() (bool, error) {
	exists, err := l.Exists()
	if err != nil {
		return false, err
	}

	if exists {
		return false, nil
	}

	if err := l.Save(DefaultConfigFor(l.platform)); err != nil {
		return false, err
	}

	return true, nil
}

// Exists checks if config file exists.
func (l *Loader) Exists() (bool, error) {
	configPath, err := l.path()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(configPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// isForeignDefault reports whether paths are the built-in paths of another
// OS, as a config saved there and synced here holds. The current OS's own
// defaults win then, so one config file works on every machine.
func (l *Loader) isForeignDefault(name string, paths []string) bool {
	for _, goos := range []string{OSDarwin, OSLinux, OSWindows} {
		if goos == l.platform.OS {
			continue
		}
		bare := Platform{OS: goos, Home: l.platform.Home, TempDir: l.platform.TempDir}
		withEnv := l.platform
		withEnv.OS = goos
		for _, p := range []Platform{bare, withEnv} {
			if def, ok := DefaultProvidersFor(p)[name]; ok && slices.Equal(def.Paths, paths) {
				return true
			}
		}
	}
	return false
}

// portableProviders drops paths that equal this OS's built-in defaults, some
// of which are machine specific (temp dir, XDG roots), so the saved file
// resolves to the right defaults on whichever OS loads it.
func (l *Loader) portableProviders(providers map[string]Provider) map[string]Provider {
	defaults := DefaultProvidersFor(l.platform)
	out := make(map[string]Provider, len(providers))
	for name := range providers {
		p := providers[name]
		if def, ok := defaults[name]; ok && slices.Equal(p.Paths, def.Paths) {
			p.Paths = nil
		}
		out[name] = p
	}
	return out
}
