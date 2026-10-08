package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
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
	// dfType restricts docker system df rows to one Type; empty sums all rows.
	dfType string
}

// NewDockerProvider creates a Docker provider with availability checking.
func NewDockerProvider(name string, cfg config.Provider) (*DockerProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	return &DockerProvider{
		BaseProvider: base,
		cleanCmd:     stripVolumesFlag(cfg.CleanCmd),
	}, nil
}

// The flag may follow whitespace, a quote or the string start; its value
// stops at whitespace, quotes and shell operators. A leading quote is kept
// so the surrounding quoting stays balanced.
var embeddedVolumesFlag = regexp.MustCompile(
	`(?:\s+|^|(['"]))--volumes(?:=[^\s'";&|()<>` + "`" + `]*)?($|[\s'";&|()<>` + "`" + `])`)

// stripVolumesFlag drops --volumes from configs written by older versions,
// whose saved clean_cmd would otherwise keep deleting volumes.
func stripVolumesFlag(cmd string) string {
	parts, err := shellquote.Split(cmd)
	if err != nil {
		return cmd
	}
	kept := parts[:0]
	for _, part := range parts {
		if part == "--volumes" || strings.HasPrefix(part, "--volumes=") {
			continue
		}
		// Wrapped commands such as sh -c carry the flag inside one token.
		kept = append(kept, removeEmbeddedVolumes(part))
	}
	return shellquote.Join(kept...)
}

// removeEmbeddedVolumes repeats the replacement because each match consumes
// the delimiter that the next adjacent flag needs.
func removeEmbeddedVolumes(part string) string {
	for {
		next := embeddedVolumesFlag.ReplaceAllString(part, "$1$2")
		if next == part {
			return part
		}
		part = next
	}
}

// dockerVolumesDFType is the docker system df row type for volumes.
const dockerVolumesDFType = "Local Volumes"

// NewDockerVolumesProvider creates a provider that prunes only Docker volumes.
func NewDockerVolumesProvider(name string, cfg config.Provider) (*DockerProvider, error) {
	p, err := NewDockerProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	p.dfType = dockerVolumesDFType
	return p, nil
}

// dockerDFRow is one line of docker system df --format '{{json .}}' output.
type dockerDFRow struct {
	Type string `json:"Type"`
	Size string `json:"Size"`
}

// CurrentSize returns actual Docker data usage from docker system df.
// Falls back to path-based size if docker system df fails.
func (p *DockerProvider) CurrentSize(ctx context.Context) (int64, error) {
	// The path fallback measures the whole Docker VM, not just volumes.
	if p.dfType != "" {
		return p.dockerDataSize(ctx)
	}
	if b, err := p.dockerDataSize(ctx); err == nil {
		return b, nil
	}
	return p.BaseProvider.CurrentSize(ctx)
}

// DiskImageSize returns the path-based filesystem size of the configured Docker paths.
func (p *DockerProvider) DiskImageSize(ctx context.Context) (int64, error) {
	return p.BaseProvider.CurrentSize(ctx)
}

func (p *DockerProvider) dockerDataSize(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
		if p.dfType != "" && row.Type != p.dfType {
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

	// The until filter does not apply to volumes, so the volumes provider
	// always runs its configured command.
	if opts.Mode == CleanModeSmart && p.dfType == "" {
		return p.smartClean(ctx, opts)
	}
	return p.fullClean(ctx, opts)
}

func (p *DockerProvider) smartClean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	hours := max(int64(p.maxAge.Hours()), 1)
	filterArg := fmt.Sprintf("until=%dh", hours)
	args := []string{"docker", "system", "prune", "-af", "--filter", filterArg}

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
