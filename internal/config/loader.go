package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Loader handles config file operations.
type Loader struct {
	v          *viper.Viper
	configPath string // override for testing, empty uses Path()
}

// NewLoader creates a new config loader.
func NewLoader() *Loader {
	return &Loader{v: viper.New()}
}

// SetConfigPath overrides config path (for testing).
func (l *Loader) SetConfigPath(path string) {
	l.configPath = path
}

func (l *Loader) path() (string, error) {
	if l.configPath != "" {
		return l.configPath, nil
	}
	return Path()
}

// Load reads config from disk and merges with defaults.
func (l *Loader) Load() (*Config, error) {
	cfg := DefaultConfig()

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

	// Merge user overrides on top of defaults
	for name, p := range userCfg.Providers {
		cfg.Providers[name] = p
	}

	return cfg, nil
}

// LoadOrCreate loads config (always merges with defaults). Returns (config, created, error).
// The created return value is deprecated and always false.
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
		"providers": cfg.Providers,
	} {
		l.v.Set(key, value)
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

	if err := l.Save(DefaultConfig()); err != nil {
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
