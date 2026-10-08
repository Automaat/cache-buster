package auto

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestAgentStatus_Launchd(t *testing.T) {
	tests := []struct {
		name      string
		plist     bool
		respond   func(string, []string) ([]byte, error)
		installed bool
		loaded    bool
		detail    string
	}{
		{"loaded", true, nil, true, true, ""},
		{"plist but not loaded", true, func(string, []string) ([]byte, error) {
			return []byte("Could not find service"), errors.New("exit status 113")
		}, true, false, "launchd has no job " + AgentLabel},
		{"nothing installed", false, func(string, []string) ([]byte, error) {
			return []byte("Could not find service"), errors.New("exit status 113")
		}, false, false, "launchd has no job " + AgentLabel},
		{"launchctl broken", true, func(string, []string) ([]byte, error) {
			return []byte("denied"), errors.New("exit status 1")
		}, true, false, "launchctl print"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &scriptedExec{respond: tt.respond}
			a := newOSAgent(t, "darwin", "/opt/bilgie", s)
			if tt.plist {
				touch(t, a.PlistPath())
			}
			st, err := a.Status(t.Context())
			require.NoError(t, err)
			assert.Equal(t, BackendLaunchd, st.Backend)
			assert.Equal(t, tt.installed, st.Installed)
			assert.Equal(t, tt.loaded, st.Loaded)
			assert.Contains(t, st.Detail, tt.detail)
			assert.Equal(t, []string{"launchctl print gui/501/" + AgentLabel}, s.commands())
		})
	}
}

func TestAgentStatus_Linux(t *testing.T) {
	t.Run("systemd timer active", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) { return []byte("active\n"), nil }}
		a := newOSAgent(t, "linux", "/usr/bin/bilgie", s)
		touch(t, a.TimerPath())
		st, err := a.Status(t.Context())
		require.NoError(t, err)
		assert.Equal(t, AgentState{Backend: BackendSystemd, Installed: true, Loaded: true}, st)
		assert.Equal(t, []string{"systemctl --user is-active " + SystemdUnit + ".timer"}, s.commands())
	})

	t.Run("systemd timer inactive", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) { return []byte("inactive\n"), errors.New("exit status 3") }}
		a := newOSAgent(t, "linux", "/usr/bin/bilgie", s)
		touch(t, a.ServicePath())
		st, err := a.Status(t.Context())
		require.NoError(t, err)
		assert.True(t, st.Installed)
		assert.False(t, st.Loaded)
		assert.Equal(t, "timer state: inactive", st.Detail)
	})

	t.Run("cron entry", func(t *testing.T) {
		line, err := RenderCronLine("/usr/bin/bilgie", "/home/u", "/home/u/auto.log", 30*time.Minute)
		require.NoError(t, err)
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) { return []byte("MAILTO=x\n" + line), nil }}
		a := newOSAgent(t, "linux", "/usr/bin/bilgie", s)
		st, err := a.Status(t.Context())
		require.NoError(t, err)
		assert.Equal(t, AgentState{Backend: BackendCron, Installed: true, Loaded: true}, st)
		assert.Equal(t, []string{"crontab -l"}, s.commands())
	})

	t.Run("no crontab", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) {
			return []byte("no crontab for u"), errors.New("exit status 1")
		}}
		st, err := newOSAgent(t, "linux", "/usr/bin/bilgie", s).Status(t.Context())
		require.NoError(t, err)
		assert.False(t, st.Installed)
	})

	t.Run("crontab missing", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) {
			return nil, &exec.Error{Name: "crontab", Err: exec.ErrNotFound}
		}}
		st, err := newOSAgent(t, "linux", "/usr/bin/bilgie", s).Status(t.Context())
		require.NoError(t, err)
		assert.False(t, st.Installed)
	})
}

func TestAgentStatus_Windows(t *testing.T) {
	t.Run("task listed", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) {
			return []byte("\"\\Other\",\"N/A\",\"Ready\"\r\n\"\\" + TaskName + "\",\"N/A\",\"Ready\"\r\n"), nil
		}}
		st, err := newOSAgent(t, "windows", `C:\bilgie.exe`, s).Status(t.Context())
		require.NoError(t, err)
		assert.Equal(t, AgentState{Backend: BackendTask, Installed: true, Loaded: true}, st)
		assert.Equal(t, []string{"schtasks /Query /FO CSV /NH"}, s.commands())
	})

	t.Run("task absent", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) { return []byte("\"\\Other\",\"N/A\",\"Ready\"\r\n"), nil }}
		st, err := newOSAgent(t, "windows", `C:\bilgie.exe`, s).Status(t.Context())
		require.NoError(t, err)
		assert.False(t, st.Installed)
	})

	t.Run("task disabled", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) {
			return []byte("\"\\" + TaskName + "\",\"N/A\",\"Disabled\"\r\n"), nil
		}}
		st, err := newOSAgent(t, "windows", `C:\bilgie.exe`, s).Status(t.Context())
		require.NoError(t, err)
		assert.True(t, st.Installed)
		assert.False(t, st.Loaded)
		assert.Equal(t, "task is disabled", st.Detail)
	})

	t.Run("query fails", func(t *testing.T) {
		s := &scriptedExec{respond: func(string, []string) ([]byte, error) { return nil, errors.New("access denied") }}
		_, err := newOSAgent(t, "windows", `C:\bilgie.exe`, s).Status(t.Context())
		require.Error(t, err)
	})
}

func TestNotifierProgram(t *testing.T) {
	assert.Equal(t, "osascript", NotifierProgram("darwin"))
	assert.Equal(t, "notify-send", NotifierProgram("linux"))
	assert.Equal(t, "powershell.exe", NotifierProgram("windows"))
	assert.Empty(t, NotifierProgram("plan9"))
}
