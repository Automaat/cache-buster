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

const tickEvery = 2 * time.Minute

var tickNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func TestInstallLaunchd_ReplacesTheV010AutoAgentWithTheTickCadence(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "darwin", "/opt/homebrew/bin/bilgie", s)
	a.Interval = 30 * time.Minute
	old, err := RenderPlist(a.Exe, a.Home, a.logPath(), a.Interval)
	require.NoError(t, err)
	old = []byte(strings.Replace(string(old), "<string>tick</string>", "<string>auto</string>", 1))
	require.NoError(t, writeFileAtomic(a.PlistPath(), old))
	a.Interval = tickEvery

	require.NoError(t, a.Install(t.Context()))

	data, err := os.ReadFile(a.PlistPath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "<string>tick</string>")
	assert.NotContains(t, string(data), "<string>auto</string>")
	assert.Contains(t, string(data), "<integer>120</integer>")

	cmds := s.commands()
	boot := indexOf(cmds, "launchctl bootout gui/501/"+AgentLabel)
	strap := indexOf(cmds, "launchctl bootstrap gui/501 "+a.PlistPath())
	require.GreaterOrEqual(t, boot, 0)
	require.Greater(t, strap, boot, "bootout must come before bootstrap")
}

func indexOf(cmds []string, want string) int {
	for i, c := range cmds {
		if c == want {
			return i
		}
	}
	return -1
}

func TestInstallLaunchd_RetriesBootstrapAfterBootout(t *testing.T) {
	failures := 2
	s := &scriptedExec{respond: func(_ string, args []string) ([]byte, error) {
		if args[0] == "bootstrap" && failures > 0 {
			failures--
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		return nil, nil
	}}
	a := newOSAgent(t, "darwin", "/opt/homebrew/bin/bilgie", s)
	a.Interval = tickEvery

	require.NoError(t, a.Install(t.Context()))

	bootstraps := 0
	for _, c := range s.commands() {
		if strings.HasPrefix(c, "launchctl bootstrap") {
			bootstraps++
		}
	}
	assert.Equal(t, 3, bootstraps)
}

func TestInstallSystemd_ReplacesTheV010TimerAndService(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "linux", "/usr/local/bin/bilgie", s)
	a.ConfigDir = filepath.Join(a.Home, ".config")
	oldService := "[Service]\nExecStart=\"/usr/local/bin/bilgie\" auto\n"
	oldTimer := "[Timer]\nOnUnitInactiveSec=1800s\n"
	require.NoError(t, writeFileAtomic(a.ServicePath(), []byte(oldService)))
	require.NoError(t, writeFileAtomic(a.TimerPath(), []byte(oldTimer)))
	a.Interval = tickEvery

	require.NoError(t, a.Install(t.Context()))

	service, err := os.ReadFile(a.ServicePath())
	require.NoError(t, err)
	timer, err := os.ReadFile(a.TimerPath())
	require.NoError(t, err)
	assert.Contains(t, string(service), `" tick`)
	assert.NotContains(t, string(service), " auto\n")
	assert.Contains(t, string(timer), "OnUnitInactiveSec=120s")
	assert.NotContains(t, string(timer), "1800s")

	cmds := strings.Join(s.commands(), "\n")
	assert.Contains(t, cmds, "systemctl --user daemon-reload")
	assert.Contains(t, cmds, "systemctl --user restart "+SystemdUnit+".timer")
}

func TestInstallCron_ReplacesTheV010LineWithATwoMinuteTickLine(t *testing.T) {
	var loaded string
	s := &scriptedExec{respond: func(name string, args []string) ([]byte, error) {
		switch {
		case name == "systemctl":
			return nil, errors.New("exit status 1")
		case name == "crontab" && args[0] == "-l":
			return []byte("0 3 * * * backup.sh\n*/30 * * * * env PATH='/usr/bin' nice -n 10 '/usr/local/bin/bilgie' auto >> '/x/auto.log' 2>&1 # bilgie\n"), nil
		case name == "crontab":
			data, err := os.ReadFile(args[0])
			loaded = string(data)
			return nil, err
		}
		return nil, nil
	}}
	a := newOSAgent(t, "linux", posixExe, s)
	a.Interval = tickEvery

	require.NoError(t, a.Install(t.Context()))

	assert.Equal(t, 1, strings.Count(loaded, "# bilgie"), loaded)
	assert.Contains(t, loaded, "*/2 * * * * ")
	assert.Contains(t, loaded, "' tick >> ")
	assert.NotContains(t, loaded, " auto >> ")
	assert.Contains(t, loaded, "0 3 * * * backup.sh\n")
}

func TestInstallTask_WritesTwoMinuteRepetitionRunningTick(t *testing.T) {
	s := &scriptedExec{}
	a := newOSAgent(t, "windows", `C:\bin\bilgie.exe`, s)
	a.Interval = tickEvery

	require.NoError(t, a.Install(t.Context()))

	data, err := os.ReadFile(a.TaskXMLPath())
	require.NoError(t, err)
	decoded := decodeUTF16(t, data)
	assert.Contains(t, decoded, "<Interval>PT2M</Interval>")
	assert.Contains(t, decoded, "<Command>cmd.exe</Command>")
	assert.Contains(t, decoded, " tick &gt;&gt; ")
	assert.Contains(t, strings.Join(s.commands(), "\n"), "schtasks /Create /TN "+TaskName+" /XML "+a.TaskXMLPath()+" /F")
}

func decodeUTF16(t *testing.T, data []byte) string {
	t.Helper()
	require.Equal(t, []byte{0xFF, 0xFE}, data[:2])
	var b strings.Builder
	for i := 2; i+1 < len(data); i += 2 {
		b.WriteRune(rune(uint16(data[i]) | uint16(data[i+1])<<8))
	}
	return b.String()
}

func TestUninstall_RemovesTickAgentOnEveryBackend(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			s := &scriptedExec{respond: func(name string, args []string) ([]byte, error) {
				if name == "schtasks" && args[0] == "/Query" {
					return []byte("\"\\bilgie\",\"N/A\",\"Ready\"\n"), nil
				}
				return nil, nil
			}}
			exe := posixExe
			if goos == "windows" {
				exe = `C:\bin\bilgie.exe`
			}
			a := newOSAgent(t, goos, exe, s)
			a.ConfigDir = filepath.Join(a.Home, ".config")
			a.Interval = tickEvery
			require.NoError(t, a.Install(t.Context()))
			require.NoError(t, WriteTickState(a.StateDir, TickState{Time: tickNow}))
			require.NoError(t, WritePassState(a.StateDir, PassState{Time: tickNow}))

			require.NoError(t, a.Uninstall(t.Context()))
			assert.NoFileExists(t, filepath.Join(a.StateDir, TickStateName))
			assert.NoFileExists(t, filepath.Join(a.StateDir, PassStateName))

			for _, p := range []string{a.PlistPath(), a.ServicePath(), a.TimerPath(), a.TaskXMLPath()} {
				assert.NoFileExists(t, p)
			}
			assert.False(t, FirstRunPending(a.StateDir))
		})
	}
}
