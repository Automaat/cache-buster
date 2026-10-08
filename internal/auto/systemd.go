package auto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const systemdNotFoundState = "not-found"

func (a Agent) unitDir() string {
	return filepath.Join(a.Home, ".config", "systemd", "user")
}

// ServicePath is where the systemd user service definition lives.
func (a Agent) ServicePath() string {
	return filepath.Join(a.unitDir(), SystemdUnit+".service")
}

// TimerPath is where the systemd user timer definition lives.
func (a Agent) TimerPath() string {
	return filepath.Join(a.unitDir(), SystemdUnit+".timer")
}

func (a Agent) systemctl(ctx context.Context, args ...string) ([]byte, error) {
	return a.Exec(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// userManagerAvailable reports whether a systemd user manager answers; a
// headless box or a container without one falls back to cron.
func (a Agent) userManagerAvailable(ctx context.Context) bool {
	_, err := a.systemctl(ctx, "show-environment")
	return err == nil
}

func (a Agent) installLinux(ctx context.Context) error {
	if a.userManagerAvailable(ctx) {
		return a.installSystemd(ctx)
	}
	return a.installCron(ctx)
}

// uninstallLinux clears both backends, since an install may have used
// either; one backend failing never stops the other from being cleaned. An
// unreadable crontab is only a warning: a user barred from cron cannot have
// installed an entry, and must still be able to uninstall the timer.
func (a Agent) uninstallLinux(ctx context.Context) (bool, error) {
	systemdRemoved, systemdErr := a.uninstallSystemd(ctx)
	cronRemoved, cronErr := a.uninstallCron(ctx)
	if cronErr != nil && systemdErr == nil {
		fmt.Fprintf(a.Out, "warning: could not check the crontab: %v\n", cronErr)
		cronErr = nil
	}
	if err := errors.Join(systemdErr, cronErr); err != nil {
		return false, err
	}
	return systemdRemoved || cronRemoved, nil
}

func (a Agent) installSystemd(ctx context.Context) error {
	service, err := RenderSystemdService(a.Exe, a.Home, a.logPath())
	if err != nil {
		return err
	}
	timer, err := RenderSystemdTimer(a.Interval)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.logPath()), 0o750); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := writeFileAtomic(a.ServicePath(), service); err != nil {
		return err
	}
	if err := writeFileAtomic(a.TimerPath(), timer); err != nil {
		return err
	}

	timerUnit := SystemdUnit + ".timer"
	for _, args := range [][]string{{"daemon-reload"}, {"enable", timerUnit}, {"restart", timerUnit}} {
		if out, err := a.systemctl(ctx, args...); err != nil {
			return fmt.Errorf("systemctl %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
		}
	}
	fmt.Fprintf(a.Out, "installed %s and %s (every %s)\n", a.ServicePath(), a.TimerPath(), a.Interval)
	if _, err := a.uninstallCron(ctx); err != nil {
		fmt.Fprintf(a.Out, "warning: could not remove an earlier crontab entry: %v\n", err)
	}
	return nil
}

// disableTimer stops and disables the timer. A timer that does not exist is
// the state the caller wants; when disable fails in an unrecognised form, a
// LoadState of not-found confirms it is absent and any other answer keeps the
// error. With no user manager reachable and no unit files on disk there is
// nothing of ours to disable, which is the cron-fallback case.
func (a Agent) disableTimer(ctx context.Context, unitsOnDisk bool) error {
	timerUnit := SystemdUnit + ".timer"
	out, err := a.systemctl(ctx, "disable", "--now", timerUnit)
	if err == nil || binaryMissing(err) {
		return nil
	}
	disableErr := fmt.Errorf("systemctl disable: %w: %s", err, strings.TrimSpace(string(out)))
	if ctx.Err() != nil {
		return disableErr
	}
	state, stateErr := a.systemctl(ctx, "show", "--property=LoadState", "--value", timerUnit)
	if stateErr != nil {
		if unitsOnDisk {
			return disableErr
		}
		return nil
	}
	if strings.TrimSpace(string(state)) != systemdNotFoundState {
		return disableErr
	}
	return nil
}

func (a Agent) uninstallSystemd(ctx context.Context) (bool, error) {
	if err := a.disableTimer(ctx, fileExists(a.TimerPath()) || fileExists(a.ServicePath())); err != nil {
		return false, err
	}
	timerGone, err := removeIfExists(a.TimerPath())
	if err != nil {
		return false, fmt.Errorf("remove timer: %w", err)
	}
	serviceGone, err := removeIfExists(a.ServicePath())
	if err != nil {
		return false, fmt.Errorf("remove service: %w", err)
	}
	removed := timerGone || serviceGone
	if removed {
		if out, err := a.systemctl(ctx, "daemon-reload"); err != nil && !binaryMissing(err) {
			return true, fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(a.Out, "removed %s and %s\n", a.ServicePath(), a.TimerPath())
	}
	return removed, nil
}

// systemdPath is the PATH the unit gets: the user manager carries none of
// the user's shell setup, yet providers shell out to go, brew, docker and friends.
func systemdPath(home string) string {
	return strings.Join([]string{
		"/home/linuxbrew/.linuxbrew/bin",
		"/usr/local/bin",
		path.Join(home, ".local", "bin"),
		path.Join(home, ".local", "share", "mise", "shims"),
		path.Join(home, "go", "bin"),
		path.Join(home, ".cargo", "bin"),
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}, ":")
}

// systemdQuote wraps s for an ExecStart value, escaping what
// systemd would otherwise expand: backslashes, quotes, specifiers and $ variables.
func systemdQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

// systemdEnvQuote is systemdQuote for Environment=, which expands specifiers
// but not $ variables.
func systemdEnvQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

// RenderSystemdService builds the oneshot unit that runs `exe auto`.
func RenderSystemdService(exe, home, logPath string) ([]byte, error) {
	if strings.ContainsAny(logPath, "\n\r") || strings.ContainsAny(exe, "\n\r") {
		return nil, fmt.Errorf("paths must not contain line breaks")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `[Unit]
Description=cache-buster automatic cache cleanup

[Service]
Type=oneshot
ExecStart=%s auto
Environment=%s
Nice=10
IOSchedulingClass=idle
StandardOutput=append:%s
StandardError=append:%s
`, systemdQuote(exe), systemdEnvQuote("PATH="+systemdPath(home)), strings.ReplaceAll(logPath, "%", "%%"), strings.ReplaceAll(logPath, "%", "%%"))
	return b.Bytes(), nil
}

// RenderSystemdTimer builds the timer that starts the service shortly after
// it is enabled and then every interval after each run finishes.
func RenderSystemdTimer(interval time.Duration) ([]byte, error) {
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return nil, fmt.Errorf("interval must be at least one second, got %s", interval)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `[Unit]
Description=Run cache-buster automatic cache cleanup every %s

[Timer]
OnActiveSec=1min
OnUnitInactiveSec=%ds
AccuracySec=1min
Unit=%s.service

[Install]
WantedBy=timers.target
`, interval, seconds, SystemdUnit)
	return b.Bytes(), nil
}
