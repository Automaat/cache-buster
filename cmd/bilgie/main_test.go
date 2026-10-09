package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_MigratesLegacyDirsBeforeASubcommandRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldState := filepath.Join(home, ".local", "state", "cache-buster")
	newState := filepath.Join(home, ".local", "state", "bilgie")
	require.NoError(t, os.MkdirAll(oldState, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(oldState, "runs.jsonl"), nil, 0o600))

	var seenNewState, seenOldState bool
	probe := &cobra.Command{
		Use: "probe",
		RunE: func(*cobra.Command, []string) error {
			_, newErr := os.Stat(filepath.Join(newState, "runs.jsonl"))
			_, oldErr := os.Stat(oldState)
			seenNewState = newErr == nil
			seenOldState = oldErr == nil
			return nil
		},
	}
	rootCmd.AddCommand(probe)
	var errOut bytes.Buffer
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"probe"})
	t.Cleanup(func() {
		rootCmd.RemoveCommand(probe)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	require.NoError(t, rootCmd.Execute())

	assert.True(t, seenNewState, "state dir must be migrated before the subcommand runs")
	assert.False(t, seenOldState)
	assert.Contains(t, errOut.String(), "migrated "+oldState+" to "+newState)
}

func TestExecute_PrintsAFailureOnce(t *testing.T) {
	probe := &cobra.Command{
		Use:  "probe-fail",
		RunE: func(*cobra.Command, []string) error { return errors.New("boom") },
	}
	rootCmd.AddCommand(probe)
	var errOut bytes.Buffer
	rootCmd.SetErr(&errOut)
	rootCmd.SetOut(&errOut)
	rootCmd.SetArgs([]string{"probe-fail"})
	t.Cleanup(func() {
		rootCmd.RemoveCommand(probe)
		rootCmd.SetErr(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})

	code := execute(&errOut)

	assert.Equal(t, 1, code)
	assert.Equal(t, 1, strings.Count(errOut.String(), "boom"), errOut.String())
}
