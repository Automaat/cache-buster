package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_MigratesLegacyDirsBeforeASubcommandRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldState := filepath.Join(home, ".local", "state", "cache-buster")
	require.NoError(t, os.MkdirAll(oldState, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(oldState, "runs.jsonl"), nil, 0o600))
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"history"})
	_ = rootCmd.Execute()

	assert.NoDirExists(t, oldState)
	assert.FileExists(t, filepath.Join(home, ".local", "state", "bilgie", "runs.jsonl"))
}
