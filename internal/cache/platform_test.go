package cache

import (
	"runtime"
	"testing"
)

// skipWithoutModeBits skips tests that need the OS to deny access through
// POSIX permission bits; Windows enforces ACLs and ignores them.
func skipWithoutModeBits(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows ignores POSIX directory mode bits")
	}
}
