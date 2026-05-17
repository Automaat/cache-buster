package provider

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/kballard/go-shellquote"
)

// CommandProvider cleans caches by running an external command.
type CommandProvider struct {
	*BaseProvider
	cleanCmd string   // original clean_cmd, kept for dry-run display
	cmdArgs  []string // clean_cmd parsed once at construction
}

// NewCommandProvider creates a provider that cleans via external command.
func NewCommandProvider(name string, cfg config.Provider) (*CommandProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	args, err := shellquote.Split(cfg.CleanCmd)
	if err != nil {
		return nil, fmt.Errorf("invalid clean_cmd: %w", err)
	}

	return &CommandProvider{
		BaseProvider: base,
		cleanCmd:     cfg.CleanCmd,
		cmdArgs:      args,
	}, nil
}

// Available reports whether the clean command's executable is on PATH.
// A configured cache path is not enough: the tool itself must be installed
// for a clean to succeed.
func (p *CommandProvider) Available() bool {
	if len(p.cmdArgs) == 0 {
		return false
	}
	_, err := exec.LookPath(p.cmdArgs[0])
	return err == nil
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

	return runMeasuredClean(ctx, p.name, p.cmdArgs, p.CurrentSize)
}
