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

	"github.com/smykla-skalski/bilgie/internal/cli"
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

func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// swapFake replaces a real subcommand with a stand-in of the same name and
// flags, so a test can prove whether the command body ran without deleting
// anything.
func swapFake(t *testing.T, orig *cobra.Command, ran *bool) {
	t.Helper()
	fake := &cobra.Command{
		Use:  orig.Name(),
		RunE: func(*cobra.Command, []string) error { *ran = true; return nil },
	}
	fake.Flags().Bool("dry-run", false, "")
	fake.Flags().String("assume-free", "", "")
	fake.Flags().Bool("force", false, "")
	fake.Flags().Bool("all", false, "")
	rootCmd.RemoveCommand(orig)
	rootCmd.AddCommand(fake)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(fake)
		rootCmd.AddCommand(orig)
	})
}

func runRootArgs(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil); rootCmd.SetArgs(nil) })
	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRootCmd_DestructiveCommandsRefuseWhileTheLegacyConfigIsPending(t *testing.T) {
	home := sandboxHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "cache-buster"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "cache-buster", "config.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "bilgie"), []byte("in the way"), 0o600))

	for _, tc := range []struct {
		real *cobra.Command
		args []string
	}{
		{cli.CleanCmd, []string{"clean", "--force", "--all"}},
		{cli.AutoCmd, []string{"auto"}},
		{cli.InstallAgentCmd, []string{"install-agent"}},
		{cli.TickCmd, []string{"tick"}},
	} {
		ran := false
		swapFake(t, tc.real, &ran)

		_, _, err := runRootArgs(t, tc.args...)

		require.Error(t, err, tc.args[0])
		assert.False(t, ran, "%s ran on default config", tc.args[0])
	}
}

func TestRootCmd_PreviewsAndStatusRunWhileTheLegacyConfigIsPending(t *testing.T) {
	home := sandboxHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "cache-buster"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "cache-buster", "config.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "bilgie"), []byte("in the way"), 0o600))

	for _, args := range [][]string{{"clean", "--dry-run"}, {"auto", "--dry-run"}, {"auto", "--assume-free", "3G"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			ran := false
			orig := map[string]*cobra.Command{"clean": cli.CleanCmd, "auto": cli.AutoCmd}[args[0]]
			swapFake(t, orig, &ran)

			_, _, err := runRootArgs(t, args...)

			require.NoError(t, err)
			assert.True(t, ran)
		})
	}
}

func TestRootCmd_HelpAndCompletionNeverMigrate(t *testing.T) {
	home := sandboxHome(t)
	oldState := filepath.Join(home, ".local", "state", "cache-buster")
	oldCfg := filepath.Join(home, ".config", "cache-buster")
	require.NoError(t, os.MkdirAll(oldState, 0o750))
	require.NoError(t, os.MkdirAll(oldCfg, 0o750))

	for _, args := range [][]string{
		{"help"}, {"help", "status"}, {"completion", "zsh"}, {"completion", "bash"},
		{"__complete", "st"}, {"__completeNoDesc", ""},
	} {
		_, stderr, err := runRootArgs(t, args...)

		require.NoError(t, err, args)
		assert.NotContains(t, stderr, "migrated", args)
		assert.DirExists(t, oldState, args)
		assert.DirExists(t, oldCfg, args)
	}
	assert.NoDirExists(t, filepath.Join(home, ".config", "bilgie"))
	assert.NoDirExists(t, filepath.Join(home, ".local", "state", "bilgie"))
}
