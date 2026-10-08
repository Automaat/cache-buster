package auto

import (
	"context"
	"encoding/csv"
	"fmt"
	"slices"
	"strings"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/provider"
)

// Scheduler backends reported by Agent.Status.
const (
	BackendLaunchd = "launchd"
	BackendSystemd = "systemd"
	BackendCron    = "cron"
	BackendTask    = "Task Scheduler"
)

// AgentState is what the OS scheduler knows about the periodic job.
type AgentState struct {
	Backend   string
	Detail    string
	Installed bool
	Loaded    bool
}

// LogFile is where the scheduled job writes its output.
func (a Agent) LogFile() string { return a.logPath() }

// Status asks the scheduler of the agent's OS whether the job is installed
// and loaded. It only reads: nothing is installed, started or removed.
func (a Agent) Status(ctx context.Context) (AgentState, error) {
	switch a.goos() {
	case goosDarwin:
		return a.statusLaunchd(ctx), nil
	case goosLinux:
		return a.statusLinux(ctx)
	case goosWindows:
		return a.statusTask(ctx)
	default:
		return AgentState{}, fmt.Errorf("agent status is not supported on %s", a.goos())
	}
}

func (a Agent) statusLaunchd(ctx context.Context) AgentState {
	st := AgentState{Backend: BackendLaunchd, Installed: fileExists(a.PlistPath())}
	target := a.domainTarget() + "/" + a.id().label
	out, err := a.Exec(ctx, "launchctl", "print", target)
	switch {
	case err == nil:
		st.Loaded = true
	case jobNotLoaded(string(out)) || exitCode(err) == serviceNotFoundExit:
		st.Detail = "launchd has no job " + a.id().label
	default:
		st.Detail = fmt.Sprintf("launchctl print: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return st
}

func (a Agent) statusLinux(ctx context.Context) (AgentState, error) {
	if fileExists(a.TimerPath()) || fileExists(a.ServicePath()) {
		st := AgentState{Backend: BackendSystemd, Installed: true}
		out, err := a.systemctl(ctx, "is-active", a.id().unit+".timer")
		state := strings.TrimSpace(string(out))
		st.Loaded = err == nil && state == "active"
		if !st.Loaded {
			st.Detail = "timer state: " + cmpOr(state, "unknown")
		}
		return st, nil
	}

	current, err := a.currentCrontab(ctx)
	if err != nil {
		if binaryMissing(err) {
			return AgentState{Backend: BackendCron}, nil
		}
		return AgentState{Backend: BackendCron}, err
	}
	_, found := withoutCronEntry(current, a.id().cronTag)
	return AgentState{Backend: BackendCron, Installed: found, Loaded: found}, nil
}

func (a Agent) statusTask(ctx context.Context) (AgentState, error) {
	out, err := a.Exec(ctx, "schtasks", "/Query", "/FO", "CSV", "/NH")
	if err != nil {
		return AgentState{Backend: BackendTask}, fmt.Errorf("schtasks query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	reader := csv.NewReader(strings.NewReader(string(out)))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return AgentState{Backend: BackendTask}, fmt.Errorf("parse schtasks query: %w", err)
	}
	for _, rec := range records {
		if len(rec) == 0 || !strings.EqualFold(strings.TrimPrefix(rec[0], `\`), a.id().task) {
			continue
		}
		st := AgentState{Backend: BackendTask, Installed: true, Loaded: true}
		if len(rec) > 2 && strings.EqualFold(strings.TrimSpace(rec[2]), "Disabled") {
			st.Loaded = false
			st.Detail = "task is disabled"
		}
		return st, nil
	}
	return AgentState{Backend: BackendTask}, nil
}

func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// NotifierProgram names the program the notifier of goos runs, or "" where
// there is no notifier.
func NotifierProgram(goos string) string {
	switch goos {
	case goosDarwin:
		return "osascript"
	case goosLinux:
		return "notify-send"
	case goosWindows:
		return "powershell.exe"
	default:
		return ""
	}
}

// Conflict is an enabled provider that auto will never run because its path
// is protected.
type Conflict struct {
	Provider string
	Reason   string
}

// ProtectionConflicts lists the enabled providers whose paths are protected,
// so auto skips them on every run.
func ProtectionConflicts(
	ctx context.Context,
	cfg *config.Config,
	home string,
	newProvider func(name string, cfg config.Provider) (provider.Provider, error),
) []Conflict {
	protected := ProtectedPaths(cfg, home)
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	slices.Sort(names)

	var out []Conflict
	for _, name := range names {
		pc := cfg.Providers[name]
		if !pc.Enabled || !cfg.Applies(name) || neverRun(name, pc) || !config.PathsExist(pc.Paths) {
			continue
		}
		p, err := newProvider(name, pc)
		if err != nil {
			continue
		}
		if _, aware := p.(provider.ProtectionAware); aware {
			continue
		}
		reason, ctxErr := protectedReason(ctx, p, pc.Type == config.TypeDirPattern, home, protected)
		if ctxErr != nil {
			break
		}
		if reason != "" {
			out = append(out, Conflict{Provider: name, Reason: reason})
		}
	}
	return out
}
