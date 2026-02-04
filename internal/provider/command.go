package provider

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

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
	if opts.DryRun {
		return CleanResult{
			Output: "would run: " + p.cleanCmd,
		}, nil
	}

	sizeBefore, _ := p.CurrentSize()

	parts, err := shellquote.Split(p.cleanCmd)
	if err != nil {
		return CleanResult{}, fmt.Errorf("invalid command: %w", err)
	}
	if len(parts) == 0 {
		return CleanResult{}, nil
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := strings.TrimSpace(stdout.String() + stderr.String())

	if err != nil {
		return CleanResult{Output: output}, err
	}

	sizeAfter, _ := p.CurrentSize()
	bytesCleaned := sizeBefore - sizeAfter
	if bytesCleaned < 0 {
		bytesCleaned = 0
	}

	return CleanResult{
		BytesCleaned: bytesCleaned,
		Output:       output,
	}, nil
}
