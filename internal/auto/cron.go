package auto

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cronSchedule maps an interval onto the five cron fields. Cron fires on
// fixed minute and hour marks, so only whole-minute intervals that divide an
// hour or a day evenly keep every gap equal; anything else is rejected
// instead of silently running on a different schedule.
func cronSchedule(interval time.Duration) (string, error) {
	if interval < time.Minute || interval%time.Minute != 0 {
		return "", fmt.Errorf("cron needs a whole number of minutes, got %s", interval)
	}
	minutes := int(interval / time.Minute)
	switch {
	case minutes < 60 && 60%minutes == 0:
		return fmt.Sprintf("*/%d * * * *", minutes), nil
	case minutes%60 == 0 && 24%(minutes/60) == 0 && minutes <= 24*60:
		if minutes == 24*60 {
			return "0 0 * * *", nil
		}
		return fmt.Sprintf("0 */%d * * *", minutes/60), nil
	default:
		return "", fmt.Errorf("cron cannot repeat every %s evenly; use a divisor of 60 minutes or 24 hours", interval)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RenderCronLine builds the crontab entry that runs `exe tick`. Percent signs
// are escaped because cron turns an unescaped one into a newline.
func RenderCronLine(exe, home, logPath string, interval time.Duration) (string, error) {
	schedule, err := cronSchedule(interval)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(exe+logPath, "\n\r") {
		return "", fmt.Errorf("paths must not contain line breaks")
	}
	command := fmt.Sprintf("env PATH=%s nice -n 10 %s tick >> %s 2>&1",
		shellQuote(systemdPath(home)), shellQuote(exe), shellQuote(logPath))
	return schedule + " " + strings.ReplaceAll(command, "%", `\%`) + cronMarker(CronTag) + "\n", nil
}

// currentCrontab returns the user's crontab; a missing crontab is empty.
func (a Agent) currentCrontab(ctx context.Context) (string, error) {
	out, err := a.Exec(ctx, "crontab", "-l")
	if err == nil {
		return string(out), nil
	}
	lower := strings.ToLower(string(out))
	// BusyBox crontab reports a missing file instead of "no crontab for user".
	if strings.Contains(lower, "no crontab") || (strings.Contains(lower, "can't open") && strings.Contains(lower, "no such file or directory")) {
		return "", nil
	}
	return "", fmt.Errorf("crontab -l: %w: %s", err, strings.TrimSpace(string(out)))
}

func cronMarker(tag string) string {
	return " # " + tag
}

func withoutCronEntry(crontab, tag string) (string, bool) {
	marker := cronMarker(tag)
	var kept []string
	found := false
	for line := range strings.SplitSeq(strings.TrimRight(crontab, "\n"), "\n") {
		if strings.HasSuffix(line, marker) {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	text := strings.Join(kept, "\n")
	if strings.TrimSpace(text) == "" {
		return "", found
	}
	return text + "\n", found
}

// loadCrontab replaces the user's crontab. crontab reads a file rather than
// stdin so the Executor needs no input channel.
func (a Agent) loadCrontab(ctx context.Context, content string) error {
	file := filepath.Join(a.StateDir, "crontab.new")
	if err := writeFileAtomic(file, []byte(content)); err != nil {
		return err
	}
	defer func() { _ = os.Remove(file) }()
	if out, err := a.Exec(ctx, "crontab", file); err != nil {
		return fmt.Errorf("crontab install: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a Agent) installCron(ctx context.Context) error {
	line, err := RenderCronLine(a.Exe, a.Home, a.logPath(), a.Interval)
	if err != nil {
		return err
	}
	current, err := a.currentCrontab(ctx)
	if binaryMissing(err) {
		return fmt.Errorf("no systemd user manager and no crontab found; cannot schedule: %w", err)
	}
	if err != nil {
		return err
	}
	rest, _ := withoutCronEntry(current, a.id().cronTag)
	if err := a.loadCrontab(ctx, rest+line); err != nil {
		return err
	}
	a.removeSystemdUnits()
	fmt.Fprintf(a.Out, "installed a crontab entry (every %s, systemd user manager not available)\n", a.Interval)
	fmt.Fprintf(a.Out, "to use a systemd timer instead, run `loginctl enable-linger $USER`, log in again and re-run install-agent\n")
	return nil
}

// uninstallCron drops the crontab entry. No crontab binary or no crontab
// means nothing was installed; a crontab that cannot be read is an error
// because the entry might still be there.
func (a Agent) uninstallCron(ctx context.Context) (bool, error) {
	current, err := a.currentCrontab(ctx)
	if err != nil {
		if binaryMissing(err) {
			return false, nil
		}
		return false, err
	}
	rest, found := withoutCronEntry(current, a.id().cronTag)
	if !found {
		return false, nil
	}
	if rest == "" {
		if out, err := a.Exec(ctx, "crontab", "-r"); err != nil {
			return false, fmt.Errorf("crontab -r: %w: %s", err, strings.TrimSpace(string(out)))
		}
	} else if err := a.loadCrontab(ctx, rest); err != nil {
		return false, err
	}
	fmt.Fprintln(a.Out, "removed the crontab entry")
	return true, nil
}

// removeSystemdUnits drops unit files of an earlier systemd install so the
// timer does not run next to the new crontab entry; the user manager is
// unreachable here, so the files are removed without disabling the timer.
func (a Agent) removeSystemdUnits() {
	timer, _ := removeIfExists(a.TimerPath())
	service, _ := removeIfExists(a.ServicePath())
	if timer || service {
		fmt.Fprintln(a.Out, "removed the systemd units of an earlier install")
	}
}
