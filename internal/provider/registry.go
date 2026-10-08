package provider

import (
	"fmt"

	"github.com/smykla-skalski/bilgie/internal/config"
)

// fileBasedProviders lists providers that clean by deleting files.
var fileBasedProviders = map[string]bool{
	"uv":                true,
	"xcode-deriveddata": true,
	"xcode-archives":    true,
	"cargo":             true,
	"gradle":            true,

	"edge":          true,
	"vivaldi":       true,
	"gh":            true,
	"vscode-shipit": true,
}

// entryBasedProviders lists providers that must delete whole top-level
// entries, because partial deletion corrupts them.
var entryBasedProviders = map[string]bool{
	"huggingface":         true,
	"playwright":          true,
	"lima":                true,
	"chrome-devtools-mcp": true,
}

// NewProvider creates a provider from config.
func NewProvider(name string, cfg config.Provider) (Provider, error) {
	if cfg.Type == config.TypeDirPattern {
		return NewDirPatternProvider(name, cfg)
	}

	if name == "docker" {
		return NewDockerProvider(name, cfg)
	}

	if name == "docker-volumes" {
		return NewDockerVolumesProvider(name, cfg)
	}

	if entryBasedProviders[name] {
		return NewEntryProvider(name, cfg)
	}

	if name == "rustup" {
		return NewRustupProvider(name, cfg)
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
