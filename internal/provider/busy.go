package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// busyProcesses maps a provider name to the tool processes whose presence
// means a clean could break a running build or install.
var busyProcesses = map[string][]string{
	"go-build": {"go"},
	"go-mod":   {"go"},
	"homebrew": {"brew"},
	"cargo":    {"cargo", "rustc"},
	"rustup":   {"rustup", "cargo", "rustc"},
	"uv":       {"uv"},
}

// busyLockFiles maps a provider name to a lock file name, relative to each
// provider path, that the tool holds with flock while it uses the cache.
var busyLockFiles = map[string]string{
	"uv": ".lock",
}

// busyGuard detects that a cache's owning tool is active.
type busyGuard struct {
	locks     []string
	processes []string

	listProcesses func(ctx context.Context) ([]string, error)
	lockHeld      func(path string) (bool, error)
}

func newBusyGuard(name string, paths []string) *busyGuard {
	procs := busyProcesses[name]
	var locks []string
	if lockName, ok := busyLockFiles[name]; ok {
		for _, p := range paths {
			locks = append(locks, filepath.Join(p, lockName))
		}
	}
	if len(procs) == 0 && len(locks) == 0 {
		return nil
	}

	return &busyGuard{
		locks:         locks,
		processes:     procs,
		listProcesses: psCommandLines,
		lockHeld:      flockHeld,
	}
}

// busyReason returns a non-empty reason when the tool is busy. A failed check
// counts as busy: cleaning blind is what the guard exists to prevent.
func (g *busyGuard) busyReason(ctx context.Context) string {
	if g == nil {
		return ""
	}

	for _, path := range g.locks {
		held, err := g.lockHeld(path)
		if err != nil {
			return fmt.Sprintf("cannot check lock %s: %v", path, err)
		}
		if held {
			return "lock held: " + path
		}
	}

	if len(g.processes) == 0 {
		return ""
	}

	lines, err := g.listProcesses(ctx)
	if err != nil {
		return "cannot list processes: " + err.Error()
	}
	for _, line := range lines {
		if proc := matchProcess(line, g.processes); proc != "" {
			return proc + " is running"
		}
	}

	return ""
}

// toolAliases maps a sibling binary to the tool it belongs to.
var toolAliases = map[string]string{"uvx": "uv"}

// matchProcess reports which wanted tool a process command line involves.
// Any token whose basename is the tool counts, wherever it sits (wrappers,
// "sh -c" strings, "xargs go vet"). It errs toward busy: a spurious skip only
// delays a clean, while a missed process could break a live build.
func matchProcess(commandLine string, wanted []string) string {
	for field := range strings.FieldsSeq(commandLine) {
		base := filepath.Base(strings.Trim(field, `"';&|()`))
		if alias, ok := toolAliases[base]; ok {
			base = alias
		}
		if base == "brew.rb" {
			base = "brew"
		}
		for _, want := range wanted {
			if base == want {
				return want
			}
		}
	}
	return ""
}

func psCommandLines(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "command=").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(bytes.TrimSpace(out)), "\n"), nil
}

// flockHeld reports whether another process holds an flock on path, using a
// non-blocking try-lock. A missing file means nobody holds it.
func flockHeld(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	fd := int(f.Fd())
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, syscall.Flock(fd, syscall.LOCK_UN)
}
