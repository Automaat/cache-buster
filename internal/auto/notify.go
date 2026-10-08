package auto

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

// Notifier shows a desktop notification. It is the seam between the run
// logic and the platform: tests inject a recorder, ports add their own.
type Notifier func(ctx context.Context, title, message string) error

// StillLow reports whether free space after the run is still under the
// configured floors. A dry-run frees nothing, so it never counts.
func StillLow(report Report, cfg config.Auto) (bool, error) {
	if report.DryRun {
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

// DefaultNotifier returns the notifier for this OS, or nil where none exists.
func DefaultNotifier(exec Executor) Notifier {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return OsascriptNotifier(exec)
}

// OsascriptNotifier shows a macOS notification. Title and message travel as
// script arguments, never spliced into the script text.
func OsascriptNotifier(exec Executor) Notifier {
	return func(ctx context.Context, title, message string) error {
		_, err := exec(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
			"-e", "end run",
			message, title)
		return err
	}
}
