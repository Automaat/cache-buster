package auto

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func legacyOf(a Agent) Agent { return a.legacyAgent() }

func TestLegacyNamesKeepThePreRenameValues(t *testing.T) {
	assert.Equal(t, "dev.mskalski.cache-buster", legacyIdentity.label)
	assert.Equal(t, "cache-buster", legacyIdentity.unit)
	assert.Equal(t, "cache-buster", legacyIdentity.cronTag)
	assert.Equal(t, "cache-buster", legacyIdentity.task)
	assert.NotEqual(t, currentIdentity, legacyIdentity)
}

func TestInstallLaunchd_RemovesLegacyPlistBeforeLoadingTheNewOne(t *testing.T) {
	rec := &recorder{}
	a := newAgent(t, rec)
	legacyPlist := legacyOf(a).PlistPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPlist), 0o750))
	require.NoError(t, os.WriteFile(legacyPlist, []byte("<plist/>"), 0o600))

	require.NoError(t, a.Install(t.Context()))

	assert.NoFileExists(t, legacyPlist)
	assert.FileExists(t, a.PlistPath())
	assert.NotEqual(t, legacyPlist, a.PlistPath())
	require.GreaterOrEqual(t, len(rec.calls), 3)
	assert.Equal(t, []string{"launchctl", "bootout", "gui/501/dev.mskalski.cache-buster"}, rec.calls[0])
	assert.Equal(t, "bootstrap", rec.calls[len(rec.calls)-1][1])
	assert.Contains(t, output(a), "removed "+legacyPlist)
	assert.True(t, FirstRunPending(a.StateDir))
}

func TestInstallLaunchd_LegacyUnloadFailureStopsBeforeInstalling(t *testing.T) {
	rec := &recorder{fail: map[string]error{
		"bootout": errors.New("exit status 5"),
		"print":   errors.New("exit status 5"),
	}}
	a := newAgent(t, rec)

	err := a.Install(t.Context())

	require.ErrorContains(t, err, "remove legacy cache-buster agent")
	assert.NoFileExists(t, a.PlistPath())
	assert.False(t, FirstRunPending(a.StateDir))
}

func TestUninstallLaunchd_RemovesLegacyPlistWithoutANewAgent(t *testing.T) {
	rec := &recorder{}
	a := newAgent(t, rec)
	legacyPlist := legacyOf(a).PlistPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPlist), 0o750))
	require.NoError(t, os.WriteFile(legacyPlist, []byte("<plist/>"), 0o600))

	require.NoError(t, a.Uninstall(t.Context()))

	assert.NoFileExists(t, legacyPlist)
	assert.NotContains(t, output(a), "not installed")
}

func TestInstallSystemd_RemovesLegacyUnits(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "linux", posixExe, s)
	legacy := legacyOf(a)
	for _, p := range []string{legacy.ServicePath(), legacy.TimerPath()} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte("[Unit]\n"), 0o600))
	}

	require.NoError(t, a.Install(t.Context()))

	assert.NoFileExists(t, legacy.ServicePath())
	assert.NoFileExists(t, legacy.TimerPath())
	assert.FileExists(t, a.ServicePath())
	assert.FileExists(t, a.TimerPath())
	cmds := s.commands()
	assert.Equal(t, "systemctl --user disable --now cache-buster.timer", cmds[0])
	assert.Contains(t, cmds, "systemctl --user enable bilgie.timer")
}

func TestInstallCron_ReplacesLegacyTaggedEntryAndKeepsOthers(t *testing.T) {
	var loaded []string
	s := &scriptedExec{}
	s.respond = func(name string, args []string) ([]byte, error) {
		switch {
		case name == "systemctl":
			return nil, errors.New("exit status 1")
		case name == "crontab" && args[0] == "-l":
			if len(loaded) > 0 {
				return []byte(loaded[len(loaded)-1]), nil
			}
			return []byte("0 3 * * * backup.sh\n*/10 * * * * old auto # cache-buster\n"), nil
		case name == "crontab":
			data, err := os.ReadFile(args[0])
			loaded = append(loaded, string(data))
			return nil, err
		}
		return nil, nil
	}
	a := newOSAgent(t, "linux", posixExe, s)
	a.Interval = 30 * time.Minute

	require.NoError(t, a.Install(t.Context()))

	require.NotEmpty(t, loaded)
	final := loaded[len(loaded)-1]
	assert.NotContains(t, final, "old auto")
	assert.NotContains(t, final, "# cache-buster")
	assert.Contains(t, final, "0 3 * * * backup.sh\n")
	assert.Equal(t, 1, strings.Count(final, "# bilgie"), final)
}

func TestUninstallCron_RemovesLegacyAndNewEntries(t *testing.T) {
	crontab := "0 3 * * * backup.sh\n*/45 * * * * x auto # cache-buster\n*/45 * * * * y auto # bilgie\n"
	s := &scriptedExec{}
	s.respond = func(name string, args []string) ([]byte, error) {
		switch {
		case name == "systemctl" && args[1] == "show":
			return []byte("not-found\n"), nil
		case name == "systemctl":
			return []byte("Unit does not exist"), errors.New("exit status 1")
		case name == "crontab" && args[0] == "-l":
			return []byte(crontab), nil
		case name == "crontab" && args[0] == "-r":
			crontab = ""
		case name == "crontab":
			data, err := os.ReadFile(args[0])
			crontab = string(data)
			return nil, err
		}
		return nil, nil
	}
	a := newOSAgent(t, "linux", posixExe, s)

	require.NoError(t, a.Uninstall(t.Context()))

	assert.Equal(t, "0 3 * * * backup.sh\n", crontab)
	assert.NotContains(t, output(a), "not installed")
}

func TestInstallTask_DeletesLegacyTaskFirst(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "windows", windowsExe, s)

	require.NoError(t, a.Install(t.Context()))

	cmds := s.commands()
	require.Len(t, cmds, 2)
	assert.Equal(t, "schtasks /Delete /TN cache-buster /F", cmds[0])
	assert.True(t, strings.HasPrefix(cmds[1], "schtasks /Create /TN bilgie "), cmds[1])
}

func TestUninstallTask_RemovesLegacyTaskEvenWhenNewIsAbsent(t *testing.T) {
	s := &scriptedExec{respond: func(_ string, args []string) ([]byte, error) {
		if args[0] == "/Delete" && args[2] == "bilgie" {
			return []byte("not found"), errors.New("exit status 1")
		}
		return []byte("\"\\Other\",\"N/A\",\"Ready\"\r\n"), nil
	}}
	a := newOSAgent(t, "windows", windowsExe, s)

	require.NoError(t, a.Uninstall(t.Context()))

	assert.NotContains(t, output(a), "not installed")
	assert.Contains(t, output(a), "removed task cache-buster")
}

func TestUninstall_UnsupportedOSIsRejectedBeforeAnyCommand(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "plan9", posixExe, s)

	require.ErrorContains(t, a.Uninstall(t.Context()), "not supported on plan9")
	assert.Empty(t, s.calls)
}
