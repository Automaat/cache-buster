package provider_test

import (
	"github.com/Automaat/cache-buster/internal/config"
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

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func macDefaults() map[string]config.Provider {
	return config.DefaultProvidersFor(config.Platform{OS: config.OSDarwin})
}
