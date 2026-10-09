package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/migrate"
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

func pick(t *testing.T, cmds map[string]*cobra.Command, name string) *cobra.Command {
	t.Helper()
	c := cmds[name]
	require.NotNil(t, c, name)
	return c
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
	pick(t, cmds, "clean").Flags().Bool("force", false, "")

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
	require.NoError(t, pick(t, cmds, "clean").Flags().Set("dry-run", "true"))
	require.NoError(t, pick(t, cmds, "auto").Flags().Set("assume-free", "3G"))
	require.NoError(t, pick(t, cmds, "tick").Flags().Set("dry-run", "true"))

	for _, name := range []string{"clean", "auto", "tick", "status", "history", "doctor", "config", "version"} {
		require.NoError(t, BeforeCommand(pick(t, cmds, name)), name)
	}
	var out bytes.Buffer
	pick(t, cmds, "status").SetErr(&out)
	require.NoError(t, BeforeCommand(pick(t, cmds, "status")))
	assert.NotContains(t, out.String(), "running on default config", "already reported today")
}

func TestBeforeCommand_ScheduledTickFailsQuietlyAfterTheFirstRefusal(t *testing.T) {
	blockedHome(t)
	tick := pick(t, guardTree("tick"), "tick")

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
	pick(t, cmds, "completion").AddCommand(zsh)
	cmds["zsh"] = zsh

	for _, name := range []string{"help", "completion", "zsh", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		var out bytes.Buffer
		pick(t, cmds, name).SetErr(&out)
		require.NoError(t, BeforeCommand(pick(t, cmds, name)), name)
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

func TestBeforeCommand_ConfigInitThroughADanglingLinkIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldCfg := filepath.Join(home, ".config", "cache-buster")
	newCfg := filepath.Join(home, ".config", "bilgie")
	outside := filepath.Join(home, "elsewhere", "config.yaml")
	require.NoError(t, os.MkdirAll(oldCfg, 0o750))
	require.NoError(t, os.MkdirAll(newCfg, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(oldCfg, "config.yaml"), []byte("providers: {}\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(newCfg, "config.yaml")))
	root := &cobra.Command{Use: "bilgie"}
	cfg := &cobra.Command{Use: "config"}
	root.AddCommand(cfg)
	initCmd := &cobra.Command{Use: "init"}
	cfg.AddCommand(initCmd)
	tick := pick(t, guardTree("tick"), "tick")

	err := BeforeCommand(initCmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, ".bak")
	assert.NoFileExists(t, outside)
	assert.Error(t, BeforeCommand(tick))
}

func TestBeforeCommand_PanicInTheHookFailsClosedForDestructiveCommandsOnly(t *testing.T) {
	t.Cleanup(func() { runLegacy = migrate.Legacy })
	runLegacy = func(string, io.Writer) []migrate.Issue { panic("boom") }
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	cmds := guardTree("clean", "tick", "history")

	for _, name := range []string{"clean", "tick"} {
		var out bytes.Buffer
		pick(t, cmds, name).SetErr(&out)
		err := BeforeCommand(pick(t, cmds, name))
		require.Error(t, err, name)
		assert.ErrorContains(t, err, "could not check", name)
		assert.Contains(t, out.String(), "boom", name)
		assert.True(t, pick(t, cmds, name).SilenceUsage, name)
	}
	var out bytes.Buffer
	pick(t, cmds, "history").SetErr(&out)
	require.NoError(t, BeforeCommand(pick(t, cmds, "history")))
	assert.Contains(t, out.String(), "warning")
}

func TestBeforeCommand_RefusalSilencesCobraUsage(t *testing.T) {
	blockedHome(t)
	c := pick(t, guardTree("clean"), "clean")

	require.Error(t, BeforeCommand(c))

	assert.True(t, c.SilenceUsage)
}
