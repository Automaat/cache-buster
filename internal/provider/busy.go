package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/kballard/go-shellquote"
	"github.com/smykla-skalski/bilgie/internal/osshim"
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

// processLister returns a lister that leaves out bilgie's own process
// and its ancestors. They carry the provider name as an argument
// ("bilgie clean cargo"), so they would always read as the tool being
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
// ancestor that is a wanted tool: its executable is the tool, or its command
// line names the tool without wrapping bilgie's own invocation
// ("cargo run -- clean", "ruby brew.rb bundle"). A wrapper such as
// "sh -c 'bilgie clean cargo'" holds that invocation and is dropped. A zero
// pid is unknown and never matches.
func excludeSelf(procs []osshim.Process, self int, wanted []string) []string {
	return excludeSelfFor(procs, self, wanted, runtime.GOOS == "windows")
}

func excludeSelfFor(procs []osshim.Process, self int, wanted []string, windows bool) []string {
	byPID := make(map[int]*osshim.Process, len(procs))
	for i := range procs {
		if procs[i].PID != 0 {
			byPID[procs[i].PID] = &procs[i]
		}
	}

	var inv invocation
	if p, ok := byPID[self]; ok {
		inv = newInvocation(commandText(p), windows)
	}

	skip := map[int]bool{}
	seen := map[int]bool{}
	for pid := self; pid != 0 && !seen[pid]; {
		seen[pid] = true
		p, ok := byPID[pid]
		if !ok {
			break
		}
		if pid == self || !keepAncestor(p, inv, wanted, windows) {
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

// commandText is the command line without any appended kernel process name.
func commandText(p *osshim.Process) string {
	if p.Args != "" {
		return p.Args
	}
	return p.CommandLine
}

// invocation is how bilgie itself was started: the executable base name and
// the whitespace-separated arguments after it.
type invocation struct {
	exe  string
	args []string
}

func newInvocation(text string, windows bool) invocation {
	norm := normalizeSeparators(text, windows)
	fields := strings.Fields(norm)
	if windows {
		if flat, ok := flattenCommand(norm, 0); ok {
			fields = flat
		}
	}
	if len(fields) < 2 {
		return invocation{}
	}
	return invocation{exe: exeName(fields[0], windows), args: fields[1:]}
}

func keepAncestor(p *osshim.Process, inv invocation, wanted []string, windows bool) bool {
	if matchProcess(firstToken(p.CommandLine), wanted) != "" {
		return true
	}
	return !wrapsInvocation(commandText(p), inv, wanted, windows) && matchProcess(p.CommandLine, wanted) != ""
}

// wrapsInvocation reports whether a command line launches bilgie with its own
// arguments, directly or through any depth of shell -c strings, with no wanted
// tool before it ("sudo cargo run -- bilgie clean"). A string that
// does not parse reports false, which keeps the ancestor and so errs busy.
func wrapsInvocation(text string, inv invocation, wanted []string, windows bool) bool {
	if inv.exe == "" {
		return false
	}
	tokens, ok := flattenCommand(normalizeSeparators(text, windows), 0)
	if !ok {
		return false
	}
	for i := 1; i+len(inv.args) <= len(tokens); i++ {
		if slices.Equal(tokens[i:i+len(inv.args)], inv.args) && exeName(tokens[i-1], windows) == inv.exe {
			return matchProcess(strings.Join(tokens[:i-1], " "), wanted) == ""
		}
	}
	return false
}

const maxWrapperDepth = 8

// flattenCommand splits text with shell quoting rules and splits every token
// that still holds whitespace again, so nested "sh -c" strings and quoted
// paths with spaces reduce to the whitespace-separated words a process listing
// shows, with shell operators glued to a word trimmed off. It reports false when any level does not parse.
func flattenCommand(text string, depth int) ([]string, bool) {
	if depth > maxWrapperDepth {
		return nil, false
	}
	words, err := shellquote.Split(text)
	if err != nil {
		return nil, false
	}
	var out []string
	for _, w := range words {
		if len(strings.Fields(w)) <= 1 {
			if t := strings.Trim(w, " \t\n;&|()"); t != "" {
				out = append(out, t)
			}
			continue
		}
		inner, ok := flattenCommand(w, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, inner...)
	}
	return out, true
}

// normalizeSeparators turns Windows backslashes into slashes, which shell
// quoting would read as escapes.
func normalizeSeparators(s string, windows bool) string {
	if windows {
		return strings.ReplaceAll(s, `\`, "/")
	}
	return s
}

func exeName(token string, windows bool) string {
	base := filepath.Base(token)
	if windows {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	return base
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
