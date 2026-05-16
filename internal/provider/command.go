package provider

import (
	"context"
	"fmt"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/kballard/go-shellquote"
)

// CommandProvider cleans caches by running an external command.
type CommandProvider struct {
	*BaseProvider
	cleanCmd string
}

// NewCommandProvider creates a provider that cleans via external command.
func NewCommandProvider(name string, cfg config.Provider) (*CommandProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	return &CommandProvider{
		BaseProvider: base,
		cleanCmd:     cfg.CleanCmd,
	}, nil
}

// Clean implements Provider.
func (p *CommandProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if opts.Mode == CleanModeSmart {
		return p.smartClean(ctx, opts)
	}
	return p.fullClean(ctx, opts)
}

func (p *CommandProvider) smartClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	trimResult, err := cache.Trim(ctx, p.paths, cache.TrimOptions{
		MaxSize: p.maxSize,
		MaxAge:  p.maxAge,
		DryRun:  opts.DryRun,
	})
	if err != nil {
		return CleanResult{}, err
	}

	return CleanResult{
		BytesCleaned: trimResult.FreedBytes,
		FilesDeleted: trimResult.DeletedCount,
		Output:       trimResult.Output,
	}, nil
}

func (p *CommandProvider) fullClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if opts.DryRun {
		return CleanResult{
			Output: "would run: " + p.cleanCmd,
		}, nil
	}

	parts, err := shellquote.Split(p.cleanCmd)
	if err != nil {
		return CleanResult{}, fmt.Errorf("invalid command: %w", err)
	}

	return runMeasuredClean(ctx, p.name, parts, p.CurrentSize)
}
