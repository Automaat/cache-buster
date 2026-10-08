package auto

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const bootstrapAttempts = 5

// AgentLabel is the launchd label and the plist file stem.
const AgentLabel = "dev.mskalski.cache-buster"

// Executor runs an external command; tests inject a recorder so nothing
// reaches the real launchctl.
type Executor func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecCommand is the Executor that runs the real command.
func ExecCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Agent installs and removes the launchd agent.
type Agent struct {
	Exec       Executor
	Out        io.Writer
	Home       string
	Exe        string
	StateDir   string
	UID        int
	Interval   time.Duration
	RetryDelay time.Duration
}

// PlistPath is where the agent definition lives.
func (a Agent) PlistPath() string {
	return filepath.Join(a.Home, "Library", "LaunchAgents", AgentLabel+".plist")
}

func (a Agent) logPath() string {
	return filepath.Join(a.Home, "Library", "Logs", "cache-buster", "auto.log")
}

func (a Agent) domainTarget() string {
	return fmt.Sprintf("gui/%d", a.UID)
}

// unload asks launchd to drop the job. A job that was never loaded is the
// state the caller wants. When bootout fails in a form not recognised as
// "not loaded", a failing `launchctl print` confirms the job is absent; any other print failure keeps the error.
func (a Agent) unload(ctx context.Context) error {
	target := a.domainTarget() + "/" + AgentLabel
	out, err := a.Exec(ctx, "launchctl", "bootout", target)
	if err == nil || jobNotLoaded(string(out)) {
		return nil
	}
	bootoutErr := fmt.Errorf("launchctl bootout: %w: %s", err, strings.TrimSpace(string(out)))
	if ctx.Err() != nil {
		return bootoutErr
	}
	printOut, printErr := a.Exec(ctx, "launchctl", "print", target)
	if printErr == nil || (!jobNotLoaded(string(printOut)) && exitCode(printErr) != serviceNotFoundExit) {
		return bootoutErr
	}
	return nil
}

// serviceNotFoundExit is the launchctl exit status for an unknown service.
const serviceNotFoundExit = 113

func exitCode(err error) int {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode()
	}
	return 0
}

func jobNotLoaded(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no such process") || strings.Contains(lower, "could not find") || strings.Contains(lower, "not found")
}

// bootstrap loads the job, retrying because bootout can return before launchd
// has finished unloading and the first bootstrap then fails with an I/O error.
func (a Agent) bootstrap(ctx context.Context) error {
	delay := a.RetryDelay
	if delay <= 0 {
		delay = time.Second
	}

	var lastErr error
	for attempt := range bootstrapAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		out, err := a.Exec(ctx, "launchctl", "bootstrap", a.domainTarget(), a.PlistPath())
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return lastErr
}

// Install arms the first-run dry-run marker, writes the plist and loads it.
// Loading unloads any earlier registration first, since bootstrap fails on a
// job that is already loaded.
func (a Agent) Install(ctx context.Context) error {
	if strings.Contains(a.Exe, "go-build") {
		return fmt.Errorf("refusing to install a temporary `go run` binary (%s); build or install cache-buster first", a.Exe)
	}
	if !filepath.IsAbs(a.Exe) {
		return fmt.Errorf("binary path must be absolute, got %q", a.Exe)
	}

	if err := os.MkdirAll(filepath.Dir(a.logPath()), 0o750); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.PlistPath()), 0o750); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := MarkFirstRunPending(a.StateDir); err != nil {
		return err
	}

	plist, err := RenderPlist(a.Exe, a.Home, a.logPath(), a.Interval)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(a.PlistPath(), plist); err != nil {
		return err
	}

	_ = a.unload(ctx)
	if err := a.bootstrap(ctx); err != nil {
		return err
	}

	fmt.Fprintf(a.Out, "installed %s (every %s)\n", a.PlistPath(), a.Interval)
	fmt.Fprintln(a.Out, "the first run is a dry-run; later runs delete")
	return nil
}

// Uninstall unloads the agent and removes its plist and first-run marker.
func (a Agent) Uninstall(ctx context.Context) error {
	if err := a.unload(ctx); err != nil {
		return err
	}

	removed := true
	if err := os.Remove(a.PlistPath()); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove plist: %w", err)
		}
		removed = false
	}
	if err := ClearFirstRun(a.StateDir); err != nil {
		return err
	}

	if removed {
		fmt.Fprintf(a.Out, "removed %s\n", a.PlistPath())
	} else {
		fmt.Fprintln(a.Out, "agent was not installed")
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write plist: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close plist: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("chmod plist: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("install plist: %w", err)
	}
	return nil
}

// agentPath is the PATH launchd gives the job. launchd carries none of the
// user's shell setup, yet providers shell out to go, brew, docker and friends.
func agentPath(home string) string {
	return strings.Join([]string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".docker", "bin"),
		"/Applications/Docker.app/Contents/Resources/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}, ":")
}

func plistString(s string) string {
	var esc bytes.Buffer
	_ = xml.EscapeText(&esc, []byte(s))
	return "<string>" + esc.String() + "</string>"
}

// RenderPlist builds the launchd property list that runs `exe auto` every interval.
func RenderPlist(exe, home, logPath string, interval time.Duration) ([]byte, error) {
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return nil, fmt.Errorf("interval must be at least one second, got %s", interval)
	}

	entries := []struct{ key, value string }{
		{"Label", plistString(AgentLabel)},
		{"ProgramArguments", "<array>\n\t\t" + plistString(exe) + "\n\t\t" + plistString("auto") + "\n\t</array>"},
		{"StartInterval", fmt.Sprintf("<integer>%d</integer>", seconds)},
		{"RunAtLoad", "<true/>"},
		{"ProcessType", plistString("Background")},
		{"LowPriorityIO", "<true/>"},
		{"Nice", "<integer>10</integer>"},
		{"StandardOutPath", plistString(logPath)},
		{"StandardErrorPath", plistString(logPath)},
		{"EnvironmentVariables", "<dict>\n\t\t<key>PATH</key>\n\t\t" + plistString(agentPath(home)) + "\n\t</dict>"},
	}

	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, e := range entries {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t%s\n", e.key, e.value)
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes(), nil
}
