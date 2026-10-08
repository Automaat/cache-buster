package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
)

func TestMigrateLegacy_ConfigAndStateFollowTheRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldConfig := filepath.Join(home, ".config", "cache-buster")
	oldState := filepath.Join(home, ".local", "state", "cache-buster")
	require.NoError(t, os.MkdirAll(oldConfig, 0o750))
	require.NoError(t, os.MkdirAll(oldState, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(oldConfig, "config.yaml"), []byte("auto:\n  interval: 20m\n"), 0o600))
	require.NoError(t, auto.MarkFirstRunPending(oldState))
	var out bytes.Buffer

	MigrateLegacy(&out)

	cfgPath, err := config.Path()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "bilgie", "config.yaml"), cfgPath)
	assert.FileExists(t, cfgPath)
	stateDir, err := auto.StateDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "state", "bilgie"), stateDir)
	assert.True(t, auto.FirstRunPending(stateDir))
	assert.NoDirExists(t, oldConfig)
	assert.NoDirExists(t, oldState)
	assert.Contains(t, out.String(), "migrated")
}
