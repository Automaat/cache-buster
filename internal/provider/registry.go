package provider

import (
	"fmt"

	"github.com/Automaat/cache-buster/internal/config"
)

// fileBasedProviders lists providers that clean by deleting files.
var fileBasedProviders = map[string]bool{
	"uv":                true,
	"xcode-deriveddata": true,
	"xcode-archives":    true,
	"cargo":             true,
	"gradle":            true,
}

// NewProvider creates a provider from config.
func NewProvider(name string, cfg config.Provider) (Provider, error) {
	if name == "docker" {
		return NewDockerProvider(name, cfg)
	}

	if name == "jetbrains" {
		return NewJetBrainsProvider(name, cfg)
	}

	if fileBasedProviders[name] {
		return NewFileProvider(name, cfg)
	}

	if cfg.CleanCmd == "" {
		return nil, fmt.Errorf("unknown provider %q requires clean_cmd", name)
	}

	return NewCommandProvider(name, cfg)
}

// LoadProviders creates all enabled providers from config.
func LoadProviders(cfg *config.Config) ([]Provider, error) {
	var providers []Provider

	for _, name := range cfg.EnabledProviders() {
		provCfg, ok := cfg.GetProvider(name)
		if !ok {
			continue
		}

		p, err := NewProvider(name, provCfg)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}

		providers = append(providers, p)
	}

	return providers, nil
}

// LoadProvider creates a single provider by name.
func LoadProvider(name string, cfg *config.Config) (Provider, error) {
	provCfg, ok := cfg.GetProvider(name)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}

	return NewProvider(name, provCfg)
}
