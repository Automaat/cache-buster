package cli

import (
	"runtime"
	"testing"
)

// skipOnWindows marks a known Windows-only failure tracked by a follow-up issue.
func skipOnWindows(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows: " + reason)
	}
}
