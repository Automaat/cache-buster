package provider

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/smykla-skalski/bilgie/internal/cache"
	"github.com/smykla-skalski/bilgie/internal/config"
)

// DefaultCleanTimeout bounds a clean command so a hung tool cannot stall a run.
const DefaultCleanTimeout = 2 * time.Minute

// CommandProvider cleans caches by running an external command.
type CommandProvider struct {
	*BaseProvider
	cleanCmd string   // original clean_cmd, kept for dry-run display
	cmdArgs  []string // clean_cmd parsed once at construction
	timeout  time.Duration
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

	timeout := DefaultCleanTimeout
	if cfg.CleanTimeout != "" {
		if strings.TrimSpace(cfg.CleanTimeout) == "" {
			return nil, fmt.Errorf("clean_timeout must not be blank")
		}
		timeout, err = config.ParseDuration(cfg.CleanTimeout)
		if err != nil {
			return nil, fmt.Errorf("parse clean_timeout: %w", err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("clean_timeout must be positive, got %q", cfg.CleanTimeout)
		}
	}

	return &CommandProvider{
		BaseProvider: base,
		cleanCmd:     cfg.CleanCmd,
		cmdArgs:      args,
		timeout:      timeout,
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
	if skipped, ok := p.skipIfBusy(ctx); ok {
		return skipped, nil
	}
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
		Entries:      entriesFromFiles(trimResult.Removed),
	}, nil
}

func (p *CommandProvider) fullClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if opts.DryRun {
		return CleanResult{
			Output: "would run: " + p.cleanCmd,
		}, nil
	}

	return runMeasuredCleanTimeout(ctx, p.name, p.cmdArgs, p.CurrentSize, p.timeout)
}
