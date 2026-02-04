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

// Load reads and validates config from disk.
func (l *Loader) Load() (*Config, error) {
	configPath, err := l.path()
	if err != nil {
		return nil, err
	}

	l.v.SetConfigFile(configPath)
	l.v.SetConfigType("yaml")

	if err := l.v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// LoadOrCreate loads config or creates default. Returns (config, created, error).
func (l *Loader) LoadOrCreate() (*Config, bool, error) {
	exists, err := l.Exists()
	if err != nil {
		return nil, false, err
	}

	if exists {
		cfg, err := l.Load()
		return cfg, false, err
	}

	cfg := DefaultConfig()
	if err := l.Save(cfg); err != nil {
		return nil, false, fmt.Errorf("create default config: %w", err)
	}

	return cfg, true, nil
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
