// Package doctor checks that the unattended agent works and that the
// configuration lets it do something useful. Diagnose is pure: every fact it
// needs (clock, free space, scheduler state, run history) arrives in Input.
package doctor

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/pkg/size"
)

// Level ranks a finding. Warn and Fail need attention and make doctor exit
// non-zero; Note is informational.
type Level int

// Finding levels.
const (
	OK Level = iota
	Note
	Warn
	Fail
)

func (l Level) String() string {
	switch l {
	case OK:
		return "ok"
	case Note:
		return "note"
	case Warn:
		return "warn"
	default:
		return "FAIL"
	}
}

// TrendWindow is how far back the free-space trend looks.
const TrendWindow = 7 * 24 * time.Hour

// staleFactor intervals without a run mean the agent is not running.
const staleFactor = 3

// skipRuns recent runs must all skip a provider for it to count as always skipped.
const skipRuns = 5

const minSkipRuns = 3

// runwayDays warns when the trend reaches min_free sooner than this.
const runwayDays = 14

const withinLimit = "within limit"

// Finding is one check result.
type Finding struct {
	Area    string
	Message string
	Hint    string
	Level   Level
}

// Input holds everything Diagnose looks at. Cfg is nil when the config could
// not be loaded and ConfigErr says why. Runs are oldest first. NotifierErr is
// set when the notifier program cannot be found.
type Input struct {
	Now time.Time

	Cfg       *config.Config
	ConfigErr error

	Runs    []auto.RunRecord
	Corrupt int

	Agent    auto.AgentState
	AgentErr error
	LogPath  string

	Free    auto.FreeSpace
	FreeErr error

	FirstRunPending bool

	NotifierProgram string
	NotifierErr     error

	Conflicts []auto.Conflict
	GOOS      string
}

// Report is the ordered list of findings.
type Report struct {
	Findings []Finding
}

// Attention counts the findings that need the user to act.
func (r Report) Attention() int {
	n := 0
	for _, f := range r.Findings {
		if f.Level >= Warn {
			n++
		}
	}
	return n
}

// Diagnose runs every check.
func Diagnose(in Input) Report {
	findings := []Finding{agentFinding(in)}
	findings = append(findings, lastRunFindings(in)...)
	findings = append(findings, freeSpaceFindings(in)...)
	findings = append(findings, configFindings(in)...)
	findings = append(findings, notifierFinding(in))
	return Report{Findings: findings}
}

func agentFinding(in Input) Finding {
	f := Finding{Area: "agent"}
	switch {
	case in.AgentErr != nil:
		f.Level = Warn
		f.Message = fmt.Sprintf("cannot query the %s scheduler: %v", in.Agent.Backend, in.AgentErr)
		f.Hint = "check that the scheduler tool is on PATH, then run doctor again"
	case !in.Agent.Installed:
		f.Level = Fail
		f.Message = "not installed: nothing runs bilgie automatically"
		f.Hint = "run: bilgie install-agent"
	case !in.Agent.Loaded:
		f.Level = Fail
		f.Message = fmt.Sprintf("installed but not loaded in %s", in.Agent.Backend)
		if in.Agent.Detail != "" {
			f.Message += " (" + in.Agent.Detail + ")"
		}
		f.Hint = "run: bilgie install-agent (it reloads the job)"
	default:
		f.Message = fmt.Sprintf("installed and loaded (%s)", in.Agent.Backend)
	}
	return f
}

func lastRunFindings(in Input) []Finding {
	if len(in.Runs) == 0 {
		level := Note
		if in.Agent.Installed {
			level = Warn
		}
		out := []Finding{{
			Area: "last run", Level: level,
			Message: "no runs recorded yet",
			Hint:    "wait for the first scheduled run, or try: bilgie auto --dry-run",
		}}
		return append(out, extraRunNotes(in)...)
	}

	last := in.Runs[len(in.Runs)-1]
	mode := ""
	if last.DryRun {
		mode = ", dry-run"
	}
	var failed []string
	for _, p := range last.Providers {
		if p.Status == auto.StatusError {
			failed = append(failed, p.Name)
		}
	}
	main := Finding{
		Area: "last run",
		Message: fmt.Sprintf("%s ago (tier %s, freed %s, %d provider error(s)%s)",
			Age(in.Now.Sub(last.Time)), last.Tier, size.FormatSize(last.FreedBytes), len(failed), mode),
	}

	switch {
	case last.Error != "":
		main.Level = Fail
		main.Message += "; run error: " + last.Error
		main.Hint = hintLog(in.LogPath, "run: bilgie auto --dry-run --verbose to reproduce")
	case len(failed) > 0:
		main.Level = Fail
		main.Message += "; failed: " + strings.Join(failed, ", ")
		main.Hint = hintLog(in.LogPath, "run: bilgie auto --dry-run --verbose to see each error")
	}

	out := []Finding{main}
	if f, ok := staleFinding(in, newestRun(in.Runs)); ok {
		out = append(out, f)
	}
	out = append(out, freedFinding(in))
	return append(out, extraRunNotes(in)...)
}

func newestRun(runs []auto.RunRecord) auto.RunRecord {
	for _, r := range slices.Backward(runs) {
		if !r.DryRun {
			return r
		}
	}
	return runs[len(runs)-1]
}

func hintLog(logPath, fallback string) string {
	if logPath == "" {
		return fallback
	}
	return "see " + logPath + "; " + fallback
}

func staleFinding(in Input, last auto.RunRecord) (Finding, bool) {
	if in.Cfg == nil || !in.Agent.Installed {
		return Finding{}, false
	}
	interval, err := in.Cfg.Auto.IntervalDuration()
	if err != nil || interval <= 0 {
		return Finding{}, false
	}
	age := in.Now.Sub(last.Time)
	if age <= staleFactor*interval {
		return Finding{}, false
	}
	return Finding{
		Area:    "schedule",
		Level:   Fail,
		Message: fmt.Sprintf("last run was %s ago but the interval is %s: the agent is not running", Age(age), interval),
		Hint:    hintLog(in.LogPath, "run: bilgie install-agent to reinstall it"),
	}, true
}

func freedFinding(in Input) Finding {
	cutoff := in.Now.Add(-TrendWindow)
	var freed int64
	runs := 0
	for _, r := range in.Runs {
		if r.DryRun || r.Time.Before(cutoff) {
			continue
		}
		if r.FreedBytes > 0 {
			freed += r.FreedBytes
			runs++
		}
	}
	return Finding{
		Area:    "freed",
		Message: fmt.Sprintf("%s freed by %d run(s) in the last 7 days", size.FormatSize(freed), runs),
	}
}

func extraRunNotes(in Input) []Finding {
	var out []Finding
	if in.Corrupt > 0 {
		out = append(out, Finding{
			Area: "history", Level: Note,
			Message: fmt.Sprintf("%d unreadable line(s) in the run log were skipped", in.Corrupt),
		})
	}
	if in.FirstRunPending {
		out = append(out, Finding{
			Area: "first run", Level: Note,
			Message: "the first run is still a dry-run: nothing is deleted until it completes",
		})
	}
	return out
}

func freeSpaceFindings(in Input) []Finding {
	if in.FreeErr != nil {
		return []Finding{{
			Area: "free space", Level: Warn,
			Message: "cannot read free space: " + in.FreeErr.Error(),
			Hint:    "check that the data volume is mounted and readable",
		}}
	}

	line := fmt.Sprintf("%s free of %s (%.0f%%)", size.FormatSize(in.Free.Free), size.FormatSize(in.Free.Total), percent(in.Free))
	f := Finding{Area: "free space", Message: line}
	if in.Cfg != nil {
		if tier, err := auto.ChooseTier(in.Free, in.Cfg.Auto); err == nil {
			f.Message += ", tier " + tier.String()
			switch tier {
			case auto.TierLow:
				f.Level = Warn
			case auto.TierCritical:
				f.Level = Fail
			case auto.TierOK:
			}
			if tier != auto.TierOK {
				f.Hint = "run: bilgie auto; run: bilgie status for large unmanaged directories"
			}
		}
	}

	out := []Finding{f}
	if t, ok := trendFinding(in); ok {
		out = append(out, t)
	}
	return out
}

func percent(fs auto.FreeSpace) float64 {
	if fs.Total <= 0 {
		return 0
	}
	return float64(fs.Free) * 100 / float64(fs.Total)
}

func trendFinding(in Input) (Finding, bool) {
	cutoff := in.Now.Add(-TrendWindow)
	var window []auto.RunRecord
	for _, r := range in.Runs {
		if !r.DryRun && !r.Time.Before(cutoff) {
			window = append(window, r)
		}
	}
	if len(window) == 0 {
		return Finding{}, false
	}
	oldest := slices.MinFunc(window, func(a, b auto.RunRecord) int { return a.Time.Compare(b.Time) })
	delta := in.Free.Free - oldest.FreeBefore
	span := in.Now.Sub(oldest.Time)

	f := Finding{
		Area: "trend",
		Message: fmt.Sprintf("free space %s over %s (%s -> %s, %d run(s))",
			signedSize(delta), Age(span), size.FormatSize(oldest.FreeBefore), size.FormatSize(in.Free.Free), len(window)),
	}
	if delta < 0 && span >= time.Hour && in.Cfg != nil {
		if minFree, err := in.Cfg.Auto.MinFreeBytes(); err == nil && in.Free.Free > minFree {
			perDay := float64(-delta) / (float64(span) / float64(24*time.Hour))
			days := float64(in.Free.Free-minFree) / perDay
			if days < runwayDays {
				f.Level = Warn
				f.Message += fmt.Sprintf("; min_free is reached in about %.0f day(s) at this rate", days)
				f.Hint = "find what grows: bilgie status shows large unmanaged directories"
			}
		}
	}
	return f, true
}

func signedSize(n int64) string {
	if n < 0 {
		return "-" + size.FormatSize(-n)
	}
	return "+" + size.FormatSize(n)
}

func configFindings(in Input) []Finding {
	if in.Cfg == nil {
		msg := "config could not be loaded"
		if in.ConfigErr != nil {
			msg += ": " + in.ConfigErr.Error()
		}
		return []Finding{{
			Area: "config", Level: Fail, Message: msg,
			Hint: "fix the config file (bilgie config show prints the active one), then run doctor again",
		}}
	}

	var out []Finding
	if f, ok := disabledFinding(in.Cfg); ok {
		out = append(out, f)
	}

	conflicted := make(map[string]bool)
	for _, c := range in.Conflicts {
		conflicted[c.Provider] = true
		out = append(out, Finding{
			Area: "config", Level: Warn,
			Message: fmt.Sprintf("%s: %s; auto never cleans it", c.Provider, c.Reason),
			Hint:    fmt.Sprintf("set providers.%s.enabled: false, or point its paths away from protected data", c.Provider),
		})
	}
	out = append(out, alwaysSkipped(in, conflicted)...)

	if !slices.ContainsFunc(out, func(f Finding) bool { return f.Level >= Warn }) {
		out = append(out, Finding{Area: "config", Message: "no provider problems found"})
	}
	return out
}

func disabledFinding(cfg *config.Config) (Finding, bool) {
	var names []string
	for name := range cfg.Providers {
		if !cfg.Providers[name].Enabled && cfg.Applies(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return Finding{}, false
	}
	slices.Sort(names)
	return Finding{
		Area: "config", Level: Note,
		Message: fmt.Sprintf("%d provider(s) disabled: %s", len(names), strings.Join(names, ", ")),
	}, true
}

// alwaysSkipped reports providers skipped for the same non-routine reason on
// each of the last runs. "within limit" is the normal idle answer.
func alwaysSkipped(in Input, conflicted map[string]bool) []Finding {
	recent := in.Runs
	if len(recent) > skipRuns {
		recent = recent[len(recent)-skipRuns:]
	}
	if len(recent) < minSkipRuns {
		return nil
	}

	reasons := make(map[string]string)
	for _, p := range recent[len(recent)-1].Providers {
		if p.Status == auto.StatusSkipped && p.Reason != withinLimit {
			reasons[p.Name] = p.Reason
		}
	}
	for _, r := range recent[:len(recent)-1] {
		for name, reason := range reasons {
			if !slices.ContainsFunc(r.Providers, func(p auto.ProviderRecord) bool {
				return p.Name == name && p.Status == auto.StatusSkipped && p.Reason == reason
			}) {
				delete(reasons, name)
			}
		}
	}

	names := make([]string, 0, len(reasons))
	for name := range reasons {
		if !conflicted[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	out := make([]Finding, 0, len(names))
	for _, name := range names {
		level := Warn
		if reasons[name] == "unavailable" {
			level = Note
		}
		out = append(out, Finding{
			Area: "config", Level: level,
			Message: fmt.Sprintf("%s was skipped on each of the last %d runs: %s", name, len(recent), reasons[name]),
			Hint:    skipHint(name, reasons[name]),
		})
	}
	return out
}

func skipHint(name, reason string) string {
	switch {
	case strings.HasPrefix(reason, "protected path"):
		return fmt.Sprintf("set providers.%s.enabled: false, or point its paths away from protected data", name)
	case reason == "unavailable":
		return fmt.Sprintf("install the tool %s needs, or set providers.%s.enabled: false", name, name)
	default:
		return fmt.Sprintf("run: bilgie auto --dry-run --verbose to see why %s is skipped", name)
	}
}

func notifierFinding(in Input) Finding {
	f := Finding{Area: "notifier"}
	switch {
	case in.NotifierProgram == "":
		f.Level = Warn
		f.Message = "no notifier exists for " + in.GOOS + ": low-space alerts will not be shown"
		f.Hint = "watch the run log or run: bilgie history"
	case in.NotifierErr != nil:
		f.Level = Warn
		f.Message = fmt.Sprintf("%s not found: low-space alerts will not be shown", in.NotifierProgram)
		f.Hint = notifierHint(in.GOOS, in.NotifierProgram)
	default:
		f.Message = in.NotifierProgram + " is available"
	}
	return f
}

func notifierHint(goos, program string) string {
	switch goos {
	case "linux":
		return "install libnotify (notify-send), for example: apt install libnotify-bin"
	case "windows":
		return "make sure powershell.exe is on PATH"
	default:
		return fmt.Sprintf("make sure %s is on PATH", program)
	}
}

// Write prints one line per finding with an indented hint for the ones that
// need attention, then a verdict line.
func (r Report) Write(w io.Writer) {
	for _, f := range r.Findings {
		fmt.Fprintf(w, "[%-4s] %s: %s\n", f.Level, f.Area, f.Message)
		if f.Hint != "" && f.Level >= Warn {
			fmt.Fprintf(w, "       what to do: %s\n", f.Hint)
		}
	}
	if n := r.Attention(); n > 0 {
		fmt.Fprintf(w, "\n%d finding(s) need attention\n", n)
	} else {
		fmt.Fprintln(w, "\nall good")
	}
}

// Age formats a duration as its largest whole unit: 5m, 3h, 2d.
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}
