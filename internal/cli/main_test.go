package cli

import (
	"os"
	"testing"
)

// TestMain points every home and config location at a scratch directory, so
// a test that forgets to set its own HOME can never read or write the real
// one.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bilgie-cli-home-")
	if err != nil {
		panic(err)
	}
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		_ = os.Setenv(key, home)
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		_ = os.Unsetenv(key)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
