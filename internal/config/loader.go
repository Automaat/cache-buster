package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Loader struct {
	v *viper.Viper
}

func NewLoader() *Loader {
	return &Loader{v: viper.New()}
}

func (l *Loader) Load() (*Config, error) {
	configPath, err := ConfigPath()
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

func (l *Loader) Save(cfg *Config) error {
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	for key, value := range map[string]any{
		"version":   cfg.Version,
		"providers": cfg.Providers,
	} {
		l.v.Set(key, value)
	}

	return l.v.WriteConfigAs(configPath)
}

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

func (l *Loader) Exists() (bool, error) {
	configPath, err := ConfigPath()
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
