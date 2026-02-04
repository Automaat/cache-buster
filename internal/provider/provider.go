package provider

import (
	"context"
	"time"
)

// CleanMode determines cleaning strategy.
type CleanMode int

// CleanMode constants.
const (
	CleanModeFull  CleanMode = iota // Delete everything (via command or all files)
	CleanModeSmart                  // LRU clean until under max_size
)

// Provider defines the interface for cache providers.
type Provider interface {
	// Name returns the provider's identifier.
	Name() string

	// Paths returns the expanded paths this provider manages.
	Paths() []string

	// CurrentSize returns the total size of cached files in bytes.
	CurrentSize() (int64, error)

	// MaxSize returns the configured maximum size in bytes.
	MaxSize() int64

	// MaxAge returns the configured maximum file age.
	MaxAge() time.Duration

	// Clean removes cached files to bring size under limit.
	Clean(ctx context.Context, opts CleanOptions) (CleanResult, error)

	// Available returns whether the provider can be used.
	// For most providers this is always true. Docker checks if daemon is running.
	Available() bool
}

// CleanOptions configures cleaning behavior.
type CleanOptions struct {
	DryRun bool
	Mode   CleanMode
}

// CleanResult contains cleaning operation results.
type CleanResult struct {
	Output       string
	BytesCleaned int64
	FilesDeleted int64
}
