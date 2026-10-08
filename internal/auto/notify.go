package auto

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

// Notifier shows a desktop notification. It is the seam between the run
// logic and the platform: tests inject a recorder, ports add their own.
type Notifier func(ctx context.Context, title, message string) error

// NotifyTimeout bounds one notifier run so a hung notifier cannot hold the run lock.
const NotifyTimeout = 10 * time.Second

// ErrNotifierUnavailable marks a notifier whose program is not installed.
// Callers log it as a skipped notification instead of a failure.
var ErrNotifierUnavailable = errors.New("notifier not available")

// StillLow reports whether free space after the run is still under the
// configured floors. A dry-run frees nothing and an unreadable final
// measurement proves nothing, so neither counts.
func StillLow(report Report, cfg config.Auto) (bool, error) {
	if report.DryRun || report.EndStale {
		return false, nil
	}
	return BelowFloor(report.End, cfg)
}

// NotifyIfStillLow sends one notification when the run left free space under
// the threshold. It reports whether a notification went out.
func NotifyIfStillLow(ctx context.Context, notify Notifier, report Report, cfg config.Auto) (bool, error) {
	if notify == nil {
		return false, nil
	}
	low, err := StillLow(report, cfg)
	if err != nil || !low {
		return false, err
	}
	msg := fmt.Sprintf("%s free of %s after cleanup. Run cache-buster status for large unmanaged directories.",
		size.FormatSize(report.End.Free), size.FormatSize(report.End.Total))
	if err := notify(ctx, "Disk space is low", msg); err != nil {
		return false, fmt.Errorf("send notification: %w", err)
	}
	return true, nil
}

// DefaultNotifier returns the bounded notifier for this OS, or nil where none exists.
func DefaultNotifier(exec Executor) Notifier {
	return notifierFor(runtime.GOOS, exec, NotifyTimeout)
}

// NotifierFor returns the notifier for goos, or nil where none exists.
func NotifierFor(goos string, exec Executor) Notifier {
	return notifierFor(goos, exec, NotifyTimeout)
}

func notifierFor(goos string, exec Executor, limit time.Duration) Notifier {
	switch goos {
	case goosDarwin:
		return bounded(OsascriptNotifier(exec), limit)
	case goosLinux:
		return bounded(NotifySendNotifier(exec), limit)
	case goosWindows:
		return bounded(PowerShellNotifier(exec), limit)
	default:
		return nil
	}
}

// bounded returns once limit passes even if the notifier ignores its context.
func bounded(notify Notifier, limit time.Duration) Notifier {
	return func(ctx context.Context, title, message string) error {
		ctx, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- notify(ctx, title, message) }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return fmt.Errorf("notification not delivered: %w", ctx.Err())
		}
	}
}

func run(ctx context.Context, exec Executor, name string, args ...string) error {
	_, err := exec(ctx, name, args...)
	if binaryMissing(err) {
		return fmt.Errorf("%w: %s", ErrNotifierUnavailable, name)
	}
	return err
}

// OsascriptNotifier shows a macOS notification. Title and message travel as
// script arguments, never spliced into the script text.
func OsascriptNotifier(exec Executor) Notifier {
	return func(ctx context.Context, title, message string) error {
		return run(ctx, exec, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
			"-e", "end run",
			message, title)
	}
}

// NotifySendNotifier shows a desktop notification on Linux. The "--" keeps a
// title or message that starts with a dash from being read as an option.
func NotifySendNotifier(exec Executor) Notifier {
	return func(ctx context.Context, title, message string) error {
		return run(ctx, exec, "notify-send", "--app-name="+agentName, "--", title, message)
	}
}

const (
	toastAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

	toastScript = `$ErrorActionPreference = 'Stop'
$title = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$message = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.UI.Notifications.ToastNotification, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$text = $xml.GetElementsByTagName('text')
$text.Item(0).AppendChild($xml.CreateTextNode($title)) | Out-Null
$text.Item(1).AppendChild($xml.CreateTextNode($message)) | Out-Null
$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show($toast)
`
)

// PowerShellNotifier shows a Windows toast. Title and message are base64
// encoded into the script and the script is passed encoded, so no text is
// ever parsed as PowerShell source.
func PowerShellNotifier(exec Executor) Notifier {
	return func(ctx context.Context, title, message string) error {
		return run(ctx, exec, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
			"-EncodedCommand", encodePowerShell(toastScriptFor(title, message)))
	}
}

func toastScriptFor(title, message string) string {
	b64 := base64.StdEncoding
	return fmt.Sprintf(toastScript, b64.EncodeToString([]byte(title)), b64.EncodeToString([]byte(message)), toastAppID)
}

func encodePowerShell(script string) string {
	return base64.StdEncoding.EncodeToString(utf16LE(script))
}
