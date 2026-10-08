package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/auto"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const autoGiB = int64(1) << 30

func oldTime() time.Time { return time.Now().Add(-48 * time.Hour) }

type autoFixture struct {
	t        *testing.T
	env      autoEnv
	loader   *config.Loader
	out      *bytes.Buffer
	sentinel string
	launchd  [][]string
	home     string
}

func newAutoFixture(t *testing.T, free int64, extraConfig string) *autoFixture {
	t.Helper()
	home := t.TempDir()
	cacheDir := filepath.Join(home, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	sentinel := filepath.Join(cacheDir, "old.bin")
	require.NoError(t, os.WriteFile(sentinel, []byte("x"), 0o600))
	require.NoError(t, os.Chtimes(sentinel, oldTime(), oldTime()))

	cfgPath := filepath.Join(home, "config.yaml")
	cfg := `version: "1"
providers:
  tool:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1G
    max_age: 1d
    clean_cmd: "true"
` + extraConfig
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()

	f := &autoFixture{t: t, loader: loader, out: &bytes.Buffer{}, sentinel: sentinel, home: home}
	f.env = autoEnv{
		free: func() (auto.FreeSpace, error) {
			return auto.FreeSpace{Free: free, Total: 1000 * autoGiB}, nil
		},
		newProvider: provider.NewProvider,
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.launchd = append(f.launchd, append([]string{name}, args...))
			return nil, nil
		},
		out:      f.out,
		stateDir: filepath.Join(home, "state"),
		home:     home,
		exe:      "/opt/homebrew/bin/cache-buster",
		uid:      501,
	}
	return f
}

func (f *autoFixture) cleaned() bool {
	_, err := os.Stat(f.sentinel)
	return os.IsNotExist(err)
}

func TestAuto_FirstRunAfterInstallDeletesNothingThenCleans(t *testing.T) {
	f := newAutoFixture(t, 1*autoGiB, "")
	require.NoError(t, auto.MarkFirstRunPending(f.env.stateDir))

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))
	assert.False(t, f.cleaned(), "first run must not delete")
	assert.Contains(t, f.out.String(), "first run after install-agent: dry-run")
	assert.False(t, auto.FirstRunPending(f.env.stateDir), "marker cleared after the dry-run")

	f.out.Reset()
	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))
	assert.True(t, f.cleaned(), "second run cleans")
}

func TestAuto_FirstRunMarkerSurvivesRestartUntilDryRunCompletes(t *testing.T) {
	f := newAutoFixture(t, 1*autoGiB, "")
	require.NoError(t, auto.MarkFirstRunPending(f.env.stateDir))
	failing := f.env
	failing.free = func() (auto.FreeSpace, error) { return auto.FreeSpace{}, os.ErrInvalid }

	require.Error(t, runAutoWithLoader(t.Context(), f.loader, failing, false))
	assert.True(t, auto.FirstRunPending(f.env.stateDir), "an aborted run keeps the marker")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))
	assert.False(t, f.cleaned())
}

func TestAuto_FirstRunAtOKTierLeavesMarkerArmed(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "auto:\n  min_free: 10G\n  min_free_pct: 5\n")
	require.NoError(t, auto.MarkFirstRunPending(f.env.stateDir))

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.Contains(t, f.out.String(), "tier ok")
	assert.False(t, f.cleaned())
	assert.True(t, auto.FirstRunPending(f.env.stateDir), "nothing was previewed, so the next run is still a dry-run")
}

func TestAuto_FirstRunWithEveryProviderFailingLeavesMarkerArmed(t *testing.T) {
	f := newAutoFixture(t, 1*autoGiB, "")
	require.NoError(t, auto.MarkFirstRunPending(f.env.stateDir))
	f.env.newProvider = func(string, config.Provider) (provider.Provider, error) {
		return nil, os.ErrInvalid
	}

	require.Error(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.True(t, auto.FirstRunPending(f.env.stateDir))
}

func TestAuto_ExplicitDryRunKeepsMarker(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	require.NoError(t, auto.MarkFirstRunPending(f.env.stateDir))
	dryRunOnly := true

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, dryRunOnly))

	assert.False(t, f.cleaned())
	assert.True(t, auto.FirstRunPending(f.env.stateDir), "a manual preview must not consume the marker")
}

func TestAuto_NoMarkerCleansOnLowSpace(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.True(t, f.cleaned())
	assert.Contains(t, f.out.String(), "tier low")
}

func TestAuto_PlentyOfSpaceAndUnderLimitDoesNothing(t *testing.T) {
	f := newAutoFixture(t, 500*autoGiB, "")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.False(t, f.cleaned())
	assert.Contains(t, f.out.String(), "tier ok")
	assert.Contains(t, f.out.String(), "within limit")
}

func TestAuto_NeverRunsDockerVolumesEvenWhenEnabled(t *testing.T) {
	volDir := t.TempDir()
	volFile := filepath.Join(volDir, "data.bin")
	require.NoError(t, os.WriteFile(volFile, []byte("x"), 0o600))
	require.NoError(t, os.Chtimes(volFile, oldTime(), oldTime()))
	f := newAutoFixture(t, 1*autoGiB, `  docker-volumes:
    enabled: true
    paths:
      - `+volDir+`
    max_size: 1
    max_age: 1d
    clean_cmd: "true"
`)

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.True(t, f.cleaned())
	assert.FileExists(t, volFile)
}

func TestAuto_CriticalSweepsDisabledDirPatternProvider(t *testing.T) {
	stale := filepath.Join(t.TempDir(), "sailx-stale")
	require.NoError(t, os.MkdirAll(stale, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "f"), []byte("x"), 0o600))
	require.NoError(t, os.Chtimes(filepath.Join(stale, "f"), oldTime(), oldTime()))
	require.NoError(t, os.Chtimes(stale, oldTime(), oldTime()))
	f := newAutoFixture(t, 1*autoGiB, `  sweep:
    enabled: false
    type: dir-pattern
    paths:
      - `+filepath.Join(filepath.Dir(stale), "sail*")+`
    max_size: 1G
    min_idle: 1h
    skip_if_open: false
`)

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.NoDirExists(t, stale)
	assert.Contains(t, f.out.String(), "tier critical")
}

func TestAuto_ConcurrentRunExits(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	lock, ok, err := auto.AcquireRunLock(f.env.stateDir)
	require.NoError(t, err)
	require.True(t, ok)
	defer lock.Release()

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.False(t, f.cleaned())
	assert.Contains(t, f.out.String(), "another auto run is in progress")
}

func TestAuto_InvalidAutoConfigIsRejected(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	cfgPath := filepath.Join(f.home, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, append(data, []byte("auto:\n  min_free_pct: 150\n")...), 0o600))

	err = runAutoWithLoader(t.Context(), f.loader, f.env, false)

	require.ErrorContains(t, err, "min_free_pct")
	assert.False(t, f.cleaned())
}

func TestAuto_ConfigOverridesChangeTier(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "auto:\n  min_free: 10G\n  min_free_pct: 5\n")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.Contains(t, f.out.String(), "tier ok")
	assert.False(t, f.cleaned())
}

func TestInstallAgent_WritesPlistWithoutRealLaunchctl(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "auto:\n  interval: 60m\n")

	require.NoError(t, runInstallAgentWithLoader(t.Context(), f.loader, f.env))

	plist := filepath.Join(f.home, "Library", "LaunchAgents", auto.AgentLabel+".plist")
	data, err := os.ReadFile(plist)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<integer>3600</integer>")
	assert.True(t, auto.FirstRunPending(f.env.stateDir))
	require.Len(t, f.launchd, 2)
	assert.Equal(t, "launchctl", f.launchd[0][0])
}

func TestInstallAgent_RejectsTooShortInterval(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "auto:\n  interval: 10s\n")

	err := runInstallAgentWithLoader(t.Context(), f.loader, f.env)

	require.ErrorContains(t, err, "interval")
	assert.Empty(t, f.launchd)
}

func TestUninstallAgent_RemovesPlist(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	require.NoError(t, runInstallAgentWithLoader(t.Context(), f.loader, f.env))
	f.launchd = nil

	require.NoError(t, f.env.agent(0).Uninstall(t.Context()))

	assert.NoFileExists(t, filepath.Join(f.home, "Library", "LaunchAgents", auto.AgentLabel+".plist"))
	assert.False(t, auto.FirstRunPending(f.env.stateDir))
	assert.Equal(t, [][]string{{"launchctl", "bootout", "gui/501/" + auto.AgentLabel}}, f.launchd)
}

func TestAssumedFree(t *testing.T) {
	base := func() (auto.FreeSpace, error) { return auto.FreeSpace{Free: 9, Total: 100}, nil }

	fn, err := assumedFree("3G", base)
	require.NoError(t, err)
	fs, err := fn()
	require.NoError(t, err)
	assert.Equal(t, 3*autoGiB, fs.Free)
	assert.Equal(t, int64(100), fs.Total)

	_, err = assumedFree("lots", base)
	require.Error(t, err)
}
