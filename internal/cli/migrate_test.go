package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
)

func TestBeforeCommand_ConfigAndStateFollowTheRename(t *testing.T) {
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
	cmd := &cobra.Command{Use: "status"}
	(&cobra.Command{Use: "bilgie"}).AddCommand(cmd)
	cmd.SetErr(&out)

	require.NoError(t, BeforeCommand(cmd))

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

func blockedHome(t *testing.T) (home, oldCfg string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldCfg = filepath.Join(home, ".config", "cache-buster")
	require.NoError(t, os.MkdirAll(oldCfg, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(oldCfg, "config.yaml"), []byte("providers: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "bilgie"), []byte("a file in the way"), 0o600))
	return home, oldCfg
}

func guardTree(names ...string) map[string]*cobra.Command {
	root := &cobra.Command{Use: "bilgie"}
	cmds := map[string]*cobra.Command{"": root}
	for _, n := range names {
		c := &cobra.Command{Use: n}
		c.Flags().Bool("dry-run", false, "")
		c.Flags().String("assume-free", "", "")
		root.AddCommand(c)
		cmds[n] = c
	}
	return cmds
}

func TestBeforeCommand_RefusesDestructiveCommandsWhileTheConfigMigrationIsPending(t *testing.T) {
	_, oldCfg := blockedHome(t)
	cmds := guardTree("clean", "auto", "install-agent", "interactive")
	cmds["clean"].Flags().Bool("force", false, "")

	for name, c := range cmds {
		var errOut bytes.Buffer
		c.SetErr(&errOut)

		err := BeforeCommand(c)

		require.Error(t, err, name)
		assert.ErrorContains(t, err, "refusing to run", name)
		assert.ErrorContains(t, err, oldCfg, name)
		assert.Regexp(t, `mv |Move-Item`, err.Error(), name)
	}
}

func TestBeforeCommand_AllowsPreviewsAndReadOnlyCommandsWithAWarningOnce(t *testing.T) {
	blockedHome(t)
	cmds := guardTree("clean", "auto", "tick", "status", "history", "doctor", "config", "version")
	require.NoError(t, cmds["clean"].Flags().Set("dry-run", "true"))
	require.NoError(t, cmds["auto"].Flags().Set("assume-free", "3G"))
	require.NoError(t, cmds["tick"].Flags().Set("dry-run", "true"))

	for _, name := range []string{"clean", "auto", "tick", "status", "history", "doctor", "config", "version"} {
		require.NoError(t, BeforeCommand(cmds[name]), name)
	}
	var out bytes.Buffer
	cmds["status"].SetErr(&out)
	require.NoError(t, BeforeCommand(cmds["status"]))
	assert.NotContains(t, out.String(), "running on default config", "already reported today")
}

func TestBeforeCommand_ScheduledTickFailsQuietlyAfterTheFirstRefusal(t *testing.T) {
	blockedHome(t)
	tick := guardTree("tick")["tick"]

	first := BeforeCommand(tick)
	second := BeforeCommand(tick)

	require.Error(t, first)
	assert.NotErrorIs(t, first, ErrReported)
	assert.ErrorIs(t, second, ErrReported)
}

func TestBeforeCommand_HelpAndCompletionNeverMoveFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldCfg := filepath.Join(home, ".config", "cache-buster")
	require.NoError(t, os.MkdirAll(oldCfg, 0o750))
	cmds := guardTree("help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd)
	zsh := &cobra.Command{Use: "zsh"}
	cmds["completion"].AddCommand(zsh)
	cmds["zsh"] = zsh

	for _, name := range []string{"help", "completion", "zsh", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		var out bytes.Buffer
		cmds[name].SetErr(&out)
		require.NoError(t, BeforeCommand(cmds[name]), name)
		assert.Empty(t, out.String(), name)
	}
	assert.DirExists(t, oldCfg)
	assert.NoDirExists(t, filepath.Join(home, ".config", "bilgie"))
}

func TestDoctor_ReportsAPendingConfigMigrationWithTheManualStep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by the shared migrate tests")
	}
	home, oldCfg := blockedHome(t)

	p := pendingLegacyConfig(home)

	require.NotNil(t, p)
	assert.Equal(t, oldCfg, p.Old)
	assert.Nil(t, pendingLegacyConfig(""))
}

func TestBeforeCommand_ConfigInitAndEditRefuseWhileTheMigrationIsPending(t *testing.T) {
	blockedHome(t)
	root := &cobra.Command{Use: "bilgie"}
	cfg := &cobra.Command{Use: "config"}
	root.AddCommand(cfg)
	for _, name := range []string{"init", "edit"} {
		c := &cobra.Command{Use: name}
		cfg.AddCommand(c)
		require.Error(t, BeforeCommand(c), name)
	}
	show := &cobra.Command{Use: "show"}
	cfg.AddCommand(show)
	require.NoError(t, BeforeCommand(show))
}
