package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var doctorNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type doctorOS struct {
	goos      string
	exe       string
	notifier  string
	installed func(t *testing.T, f *autoFixture)
	respond   func(name string, args []string) ([]byte, error)
}

func scheduled(goos string) doctorOS {
	switch goos {
	case "darwin":
		return doctorOS{goos: goos, exe: "/opt/homebrew/bin/bilgie", notifier: "osascript",
			installed: func(t *testing.T, f *autoFixture) {
				t.Helper()
				path := f.env.agent(0).PlistPath()
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
			},
			respond: func(string, []string) ([]byte, error) { return nil, nil },
		}
	case "linux":
		return doctorOS{goos: goos, exe: "/usr/local/bin/bilgie", notifier: "notify-send",
			installed: func(t *testing.T, f *autoFixture) {
				t.Helper()
				path := f.env.agent(0).TimerPath()
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
			},
			respond: func(string, []string) ([]byte, error) { return []byte("active\n"), nil },
		}
	default:
		return doctorOS{goos: goos, exe: `C:\bin\bilgie.exe`, notifier: "powershell.exe",
			respond: func(string, []string) ([]byte, error) {
				return []byte("\"\\" + auto.TaskName + "\",\"N/A\",\"Ready\"\r\n"), nil
			},
		}
	}
}

func doctorFixture(t *testing.T, d doctorOS, free int64, runs ...auto.RunRecord) *autoFixture {
	t.Helper()
	f := newAutoFixture(t, free, "auto:\n  interval: 30m\n  min_free: 30G\n  min_free_pct: 0\n")
	f.env.goos = d.goos
	f.env.exe = d.exe
	f.env.now = func() time.Time { return doctorNow }
	f.env.exec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return d.respond(name, args)
	}
	f.env.lookPath = func(name string) (string, error) {
		if name == d.notifier {
			return "/fake/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	if d.installed != nil {
		d.installed(t, f)
	}
	for _, r := range runs {
		require.NoError(t, auto.AppendRun(f.env.stateDir, r))
	}
	return f
}

func recentRun() auto.RunRecord {
	return auto.RunRecord{
		Time: doctorNow.Add(-15 * time.Minute), Tier: "ok",
		FreeBefore: 100 * autoGiB, FreeAfter: 100 * autoGiB, FreedBytes: autoGiB,
		Providers: []auto.ProviderRecord{{Name: "tool", Status: auto.StatusSkipped, Reason: "within limit"}},
	}
}

func TestDoctor_HealthyOnEveryOS(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			d := scheduled(goos)
			f := doctorFixture(t, d, 100*autoGiB, recentRun())

			err := runDoctorWithLoader(t.Context(), f.loader, f.env)

			require.NoError(t, err, f.out.String())
			out := f.out.String()
			assert.Contains(t, out, "[ok  ] agent: installed and loaded")
			assert.Contains(t, out, "last run: 15m ago (tier ok, freed 1.0 GiB")
			assert.Contains(t, out, "free space: 100 GiB free of 1000 GiB (10%), tier ok")
			assert.Contains(t, out, "[ok  ] notifier: "+d.notifier+" is available")
			assert.Contains(t, out, "all good")
		})
	}
}

func TestDoctor_ExitsNonZeroWithHintsOnEveryOS(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			d := scheduled(goos)
			d.installed = nil
			d.respond = func(name string, _ []string) ([]byte, error) {
				switch name {
				case "schtasks":
					return []byte("\"\\Other\",\"N/A\",\"Ready\"\r\n"), nil
				case "crontab":
					return []byte("no crontab for u"), errors.New("exit status 1")
				}
				return []byte("Could not find service"), errors.New("exit status 113")
			}
			f := doctorFixture(t, d, 10*autoGiB)
			f.env.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

			err := runDoctorWithLoader(t.Context(), f.loader, f.env)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "finding(s) need attention")
			out := f.out.String()
			assert.Contains(t, out, "[FAIL] agent: not installed")
			assert.Contains(t, out, "what to do: run: bilgie install-agent")
			assert.Contains(t, out, "tier low")
			assert.Contains(t, out, "[warn] notifier: "+d.notifier+" not found")
			assert.Contains(t, out, "no runs recorded yet")
		})
	}
}

func TestDoctor_ReportsFailedLastRunAndStaleAgent(t *testing.T) {
	d := scheduled("linux")
	failed := recentRun()
	failed.Time = doctorNow.Add(-6 * time.Hour)
	failed.Error = "read free space: boom"
	f := doctorFixture(t, d, 100*autoGiB, failed)

	err := runDoctorWithLoader(t.Context(), f.loader, f.env)

	require.Error(t, err)
	out := f.out.String()
	assert.Contains(t, out, "run error: read free space: boom")
	assert.Contains(t, out, "last run was 6h ago but the interval is 30m0s")
	assert.Contains(t, out, filepath.Join(f.env.stateDir, "auto.log"))
}

func TestDoctor_UsesTrendFromRunHistory(t *testing.T) {
	d := scheduled("linux")
	old := recentRun()
	old.Time = doctorNow.Add(-4 * 24 * time.Hour)
	old.FreeBefore = 160 * autoGiB
	f := doctorFixture(t, d, 100*autoGiB, old, recentRun())

	err := runDoctorWithLoader(t.Context(), f.loader, f.env)

	require.Error(t, err)
	assert.Contains(t, f.out.String(), "trend: free space -60 GiB over 4d (160 GiB -> 100 GiB, 2 run(s)); min_free is reached in about 5 day(s)")
}

func TestDoctor_FlagsProtectedProviderAndBadConfig(t *testing.T) {
	d := scheduled("linux")
	f := doctorFixture(t, d, 100*autoGiB, recentRun())
	protectedDir := filepath.Join(f.home, "Downloads", "cache")
	require.NoError(t, os.MkdirAll(protectedDir, 0o750))
	cfg := `version: "1"
auto:
  interval: 30m
providers:
  dl:
    enabled: true
    paths: [` + protectedDir + `]
    max_size: 1G
    clean_cmd: "true"
  off:
    enabled: false
    paths: [` + f.home + `]
    max_size: 1G
`
	require.NoError(t, os.WriteFile(filepath.Join(f.home, "config.yaml"), []byte(cfg), 0o600))

	err := runDoctorWithLoader(t.Context(), f.loader, f.env)

	require.Error(t, err)
	out := f.out.String()
	assert.Contains(t, out, "dl: protected path "+protectedDir)
	assert.Contains(t, out, "1 provider(s) disabled: off")
	assert.Contains(t, out, "providers.dl.enabled: false")

	require.NoError(t, os.WriteFile(filepath.Join(f.home, "config.yaml"), []byte("auto:\n  interval: nope\n"), 0o600))
	f.out.Reset()
	err = runDoctorWithLoader(t.Context(), f.loader, f.env)
	require.Error(t, err)
	assert.Contains(t, f.out.String(), "[FAIL] config: config could not be loaded")
	assert.True(t, strings.Contains(f.out.String(), "agent: installed and loaded"), "other checks still run")
}
