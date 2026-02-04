package provider

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
	"github.com/kballard/go-shellquote"
)

// DockerProvider cleans Docker caches when daemon is available.
type DockerProvider struct {
	*BaseProvider
	cleanCmd string
}

// NewDockerProvider creates a Docker provider with availability checking.
func NewDockerProvider(name string, cfg config.Provider) (*DockerProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	return &DockerProvider{
		BaseProvider: base,
		cleanCmd:     cfg.CleanCmd,
	}, nil
}

// Available implements Provider.
func (p *DockerProvider) Available() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}

	cmd := exec.Command("docker", "ps", "--quiet")
	return cmd.Run() == nil
}

// Clean implements Provider.
func (p *DockerProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if !p.Available() {
		return CleanResult{
			Output: "docker not available",
		}, nil
	}

	if opts.Mode == CleanModeSmart {
		return p.smartClean(ctx, opts)
	}
	return p.fullClean(ctx, opts)
}

func (p *DockerProvider) smartClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	keepStorage := size.FormatSize(p.maxSize)
	smartCmd := fmt.Sprintf("docker buildx prune -af --keep-storage=%s", keepStorage)

	if opts.DryRun {
		return CleanResult{
			Output: "would run: " + smartCmd,
		}, nil
	}

	sizeBefore, _ := p.CurrentSize()

	cmd := exec.CommandContext(ctx, "docker", "buildx", "prune", "-af", "--keep-storage="+keepStorage)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
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

func (p *DockerProvider) fullClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
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
