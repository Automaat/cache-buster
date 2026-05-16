package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

// dockerDFRow is one line of docker system df --format '{{json .}}' output.
type dockerDFRow struct {
	Size string `json:"Size"`
}

// CurrentSize returns actual Docker data usage from docker system df.
// Falls back to path-based size if docker system df fails.
func (p *DockerProvider) CurrentSize() (int64, error) {
	if b, err := p.dockerDataSize(); err == nil {
		return b, nil
	}
	return p.BaseProvider.CurrentSize()
}

// DiskImageSize returns the path-based filesystem size of the configured Docker paths.
func (p *DockerProvider) DiskImageSize() (int64, error) {
	return p.BaseProvider.CurrentSize()
}

func (p *DockerProvider) dockerDataSize() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return 0, fmt.Errorf("docker system df: %w: %s", err, msg)
		}
		return 0, fmt.Errorf("docker system df: %w", err)
	}

	var total int64
	var firstErr error
	var rowsParsed int

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var row dockerDFRow
		if jsonErr := json.Unmarshal([]byte(line), &row); jsonErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("unmarshal docker df line %q: %w", line, jsonErr)
			}
			continue
		}
		b, parseErr := size.ParseSize(row.Size)
		if parseErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("parse docker size %q: %w", row.Size, parseErr)
			}
			continue
		}
		total += b
		rowsParsed++
	}

	if rowsParsed == 0 {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, fmt.Errorf("docker system df: no parsable output")
	}

	// Some rows parsed successfully; treat partial parse errors as non-fatal.
	return total, nil
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
	hours := max(int64(p.maxAge.Hours()), 1)
	filterArg := fmt.Sprintf("until=%dh", hours)
	args := []string{"docker", "system", "prune", "-af", "--volumes", "--filter", filterArg}

	if opts.DryRun {
		return CleanResult{
			Output: "would run: " + strings.Join(args, " "),
		}, nil
	}

	return runMeasuredClean(ctx, p.name, args, p.CurrentSize)
}

func (p *DockerProvider) fullClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
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
