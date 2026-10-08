package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Automaat/cache-buster/internal/osshim"
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
		listProcesses: processLister(procs),
		lockHeld:      osshim.LockHeld,
	}
}

// processLister returns a lister that leaves out cache-buster's own process
// and its ancestors. They carry the provider name as an argument
// ("cache-buster clean cargo"), so they would always read as the tool being
// busy.
func processLister(wanted []string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		procs, err := osshim.ProcessTable(ctx)
		if err != nil {
			return nil, err
		}
		return excludeSelf(procs, os.Getpid(), wanted), nil
	}
}

// excludeSelf drops the process with pid self and its ancestors, except an
// ancestor whose executable is itself a wanted tool ("cargo run -- clean"):
// that tool is really running. A zero pid is unknown and never matches.
func excludeSelf(procs []osshim.Process, self int, wanted []string) []string {
	byPID := make(map[int]*osshim.Process, len(procs))
	for i := range procs {
		if procs[i].PID != 0 {
			byPID[procs[i].PID] = &procs[i]
		}
	}

	skip := map[int]bool{}
	for pid := self; pid != 0 && !skip[pid]; {
		p, ok := byPID[pid]
		if !ok {
			break
		}
		if pid == self || matchProcess(firstToken(p.CommandLine), wanted) == "" {
			skip[pid] = true
		}
		pid = p.PPID
	}

	lines := make([]string, 0, len(procs))
	for i := range procs {
		if procs[i].PID != 0 && skip[procs[i].PID] {
			continue
		}
		lines = append(lines, procs[i].CommandLine)
	}
	return lines
}

func firstToken(commandLine string) string {
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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
		if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(base), ".exe") {
			base = strings.ToLower(base[:len(base)-len(".exe")])
		}
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
