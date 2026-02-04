package provider

import (
	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

// BaseProvider implements common functionality for providers.
type BaseProvider struct {
	name    string
	paths   []string
	maxSize int64
}

// NewBaseProvider creates a BaseProvider from config.
func NewBaseProvider(name string, cfg config.Provider) (*BaseProvider, error) {
	paths, err := config.ExpandPaths(cfg.Paths)
	if err != nil {
		return nil, err
	}

	maxBytes, err := size.ParseSize(cfg.MaxSize)
	if err != nil {
		return nil, err
	}

	return &BaseProvider{
		name:    name,
		paths:   paths,
		maxSize: maxBytes,
	}, nil
}

// Name implements Provider.
func (b *BaseProvider) Name() string {
	return b.name
}

// Paths implements Provider.
func (b *BaseProvider) Paths() []string {
	return b.paths
}

// CurrentSize implements Provider.
func (b *BaseProvider) CurrentSize() (int64, error) {
	return cache.CalculateSize(b.paths)
}

// MaxSize implements Provider.
func (b *BaseProvider) MaxSize() int64 {
	return b.maxSize
}

// Available implements Provider.
func (b *BaseProvider) Available() bool {
	return true
}
