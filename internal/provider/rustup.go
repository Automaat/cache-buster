package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
)

// rustupRunner runs rustup with args and returns its combined output.
type rustupRunner func(ctx context.Context, args ...string) (string, error)

// RustupProvider uninstalls rustup toolchains other than stable and the default.
type RustupProvider struct {
	*BaseProvider
	run rustupRunner
	// timeout bounds each rustup invocation.
	timeout time.Duration
}

// NewRustupProvider creates a provider that uninstalls unused toolchains.
func NewRustupProvider(name string, cfg config.Provider) (*RustupProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	timeout := DefaultCleanTimeout
	if cfg.CleanTimeout != "" {
		timeout, err = config.ParseDuration(cfg.CleanTimeout)
		if err != nil {
			return nil, fmt.Errorf("parse clean_timeout: %w", err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("clean_timeout must be positive, got %q", cfg.CleanTimeout)
		}
	}
	return &RustupProvider{BaseProvider: base, run: execRustup, timeout: timeout}, nil
}

func execRustup(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "rustup", args...)
	// Without a delay a child holding the pipes keeps Run blocked after the
	// context kills rustup.
	cmd.WaitDelay = cleanWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Stdout only: stderr notices must never be parsed as toolchain rows.
	if err != nil {
		return strings.TrimSpace(stdout.String() + stderr.String()), err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ownsRustupHome reports whether home is the directory holding a configured
// toolchains path, so measured size and rustup's own view agree.
func (p *RustupProvider) ownsRustupHome(home string) bool {
	for _, path := range p.paths {
		if filepath.Clean(filepath.Dir(path)) == filepath.Clean(home) {
			return true
		}
	}
	return false
}

func (p *RustupProvider) runBounded(ctx context.Context, args ...string) (string, error) {
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	return p.run(ctx, args...)
}

// Available reports whether rustup is on PATH.
func (p *RustupProvider) Available() bool {
	_, err := exec.LookPath("rustup")
	return err == nil
}

// Clean implements Provider. Smart and full modes behave the same: a
// toolchain is removed whole or not at all.
func (p *RustupProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if skipped, ok := p.skipIfBusy(ctx); ok {
		return skipped, nil
	}

	if home := os.Getenv("RUSTUP_HOME"); home != "" && !p.ownsRustupHome(home) {
		return CleanResult{}, fmt.Errorf("RUSTUP_HOME %q is not the parent of the configured paths", home)
	}

	current, sizeErr := p.CurrentSize(ctx)
	if sizeErr != nil {
		return CleanResult{}, fmt.Errorf("measure toolchains: %w", sizeErr)
	}
	if current <= p.maxSize {
		return CleanResult{Output: "already under limit"}, nil
	}

	listing, err := p.runBounded(ctx, "toolchain", "list")
	if err != nil {
		return CleanResult{Output: listing}, fmt.Errorf("rustup toolchain list: %w", err)
	}

	overrides, err := p.runBounded(ctx, "override", "list")
	if err != nil {
		return CleanResult{Output: overrides}, fmt.Errorf("rustup override list: %w", err)
	}

	removable, err := removableToolchains(listing, overrides)
	if err != nil {
		return CleanResult{}, err
	}
	if len(removable) == 0 {
		return CleanResult{Output: "no old toolchains to remove"}, nil
	}

	if opts.DryRun {
		return CleanResult{Output: "would uninstall: " + strings.Join(removable, ", ")}, nil
	}

	var removed []string
	for _, tc := range removable {
		if ctx.Err() != nil {
			return CleanResult{Output: "interrupted"}, ctx.Err()
		}
		if out, err := p.runBounded(ctx, "toolchain", "uninstall", tc); err != nil {
			res := p.rustupResult(ctx, current, removed)
			res.Output += "\n" + out
			return res, fmt.Errorf("uninstall %s: %w", tc, err)
		}
		removed = append(removed, tc)
	}

	return p.rustupResult(ctx, current, removed), nil
}

// rustupResult reports what was uninstalled so far and the bytes freed.
func (p *RustupProvider) rustupResult(ctx context.Context, before int64, removed []string) CleanResult {
	var freed int64
	if after, err := p.CurrentSize(ctx); err == nil && before > after {
		freed = before - after
	}
	return CleanResult{
		BytesCleaned: freed,
		Output:       "uninstalled: " + strings.Join(removed, ", "),
	}
}

// removableToolchains parses `rustup toolchain list` and returns toolchains
// safe to uninstall. It fails closed: without an identifiable default
// toolchain nothing is removed.
func removableToolchains(listing, overrides string) ([]string, error) {
	pinned := overrideToolchains(overrides)
	var (
		names      []string
		keep       = map[string]bool{}
		hasDefault bool
	)
	for line := range strings.SplitSeq(listing, "\n") {
		fields := strings.Fields(line)
		if strings.HasPrefix(strings.TrimSpace(line), "no installed toolchains") {
			continue
		}
		if len(fields) == 0 || !toolchainNamePattern.MatchString(fields[0]) {
			continue
		}
		name := fields[0]
		names = append(names, name)

		marker := toolchainMarker(fields[1:])
		// A trailing path marks a linked toolchain: not ours to unlink.
		if len(fields) > 1 && !strings.HasPrefix(fields[1], "(") {
			keep[name] = true
		}
		if strings.Contains(marker, "default") {
			hasDefault = true
			keep[name] = true
		}
		if isPinned(name, pinned) || strings.Contains(marker, "active") || strings.Contains(marker, "override") || isStableToolchain(name) {
			keep[name] = true
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	if !hasDefault {
		return nil, errors.New("rustup: no default toolchain found, refusing to remove any")
	}

	var removable []string
	for _, name := range names {
		if !keep[name] {
			removable = append(removable, name)
		}
	}
	return removable, nil
}

var toolchainNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// toolchainMarker returns the parenthesised suffix of a listing row. A
// linked toolchain's path is not a marker.
func toolchainMarker(rest []string) string {
	if len(rest) == 0 || !strings.HasPrefix(rest[0], "(") {
		return ""
	}
	joined := strings.Join(rest, " ")
	if !strings.HasSuffix(joined, ")") {
		return ""
	}
	return joined
}

// overrideToolchains returns the toolchain names pinned by directory
// overrides, from `rustup override list` rows of "<path> <toolchain>".
func overrideToolchains(overrides string) []string {
	var pinned []string
	for line := range strings.SplitSeq(overrides, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if tc := fields[len(fields)-1]; toolchainNamePattern.MatchString(tc) {
			pinned = append(pinned, tc)
		}
	}
	return pinned
}

// isPinned matches full names and bare channels such as 1.75.0 or nightly.
func isPinned(name string, pinned []string) bool {
	for _, p := range pinned {
		if name == p || strings.HasPrefix(name, p+"-") {
			return true
		}
	}
	return false
}

func isStableToolchain(name string) bool {
	return name == "stable" || strings.HasPrefix(name, "stable-")
}
