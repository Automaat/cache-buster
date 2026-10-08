package doctor

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gib = int64(1) << 30

var fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func testConfig(disabled ...string) *config.Config {
	cfg := &config.Config{
		Auto:      config.Auto{Interval: "30m", MinFree: "30G"},
		Providers: map[string]config.Provider{"go": {Enabled: true}, "npm": {Enabled: true}},
	}
	for _, name := range disabled {
		cfg.Providers[name] = config.Provider{Enabled: false}
	}
	return cfg
}

func healthy() Input {
	return Input{
		Now:             fixedNow,
		GOOS:            "linux",
		Cfg:             testConfig(),
		Agent:           auto.AgentState{Backend: auto.BackendSystemd, Installed: true, Loaded: true},
		Free:            auto.FreeSpace{Free: 200 * gib, Total: 500 * gib},
		NotifierProgram: "notify-send",
		Runs: []auto.RunRecord{{
			Time: fixedNow.Add(-20 * time.Minute), Tier: "ok",
			FreeBefore: 200 * gib, FreeAfter: 200 * gib, FreedBytes: gib,
		}},
	}
}

func find(t *testing.T, r Report, area string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Area == area {
			return f
		}
	}
	require.Failf(t, "finding missing", "area %q in %+v", area, r.Findings)
	return Finding{}
}

func TestDiagnose_HealthyNeedsNoAttention(t *testing.T) {
	r := Diagnose(healthy())
	assert.Zero(t, r.Attention(), "%+v", r.Findings)

	var buf bytes.Buffer
	r.Write(&buf)
	assert.Contains(t, buf.String(), "[ok  ] agent: installed and loaded (systemd)")
	assert.Contains(t, buf.String(), "last run: 20m ago (tier ok, freed 1.0 GiB, 0 provider error(s))")
	assert.Contains(t, buf.String(), "all good")
}

func TestDiagnose_Findings(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Input)
		area     string
		contains string
		hint     string
		level    Level
	}{
		{"agent missing", func(in *Input) { in.Agent = auto.AgentState{Backend: auto.BackendLaunchd} },
			"agent", "not installed", "bilgie install-agent", Fail},
		{"agent not loaded", func(in *Input) {
			in.Agent = auto.AgentState{Backend: auto.BackendLaunchd, Installed: true, Detail: "launchd has no job x"}
		}, "agent", "installed but not loaded in launchd (launchd has no job x)", "bilgie install-agent", Fail},
		{"scheduler error", func(in *Input) { in.AgentErr = errors.New("boom") },
			"agent", "cannot query", "PATH", Warn},
		{"no runs while installed", func(in *Input) { in.Runs = nil },
			"last run", "no runs recorded yet", "auto --dry-run", Warn},
		{"no runs, no agent", func(in *Input) { in.Runs = nil; in.Agent = auto.AgentState{} },
			"last run", "no runs recorded yet", "", Note},
		{"run error", func(in *Input) { in.Runs[0].Error = "read free space: eof"; in.LogPath = "/l/auto.log" },
			"last run", "run error: read free space: eof", "/l/auto.log", Fail},
		{"provider error", func(in *Input) {
			in.Runs[0].Providers = []auto.ProviderRecord{{Name: "go", Status: auto.StatusError, Error: "x"}}
		}, "last run", "; failed: go", "--verbose", Fail},
		{"stale agent", func(in *Input) { in.Runs[0].Time = fixedNow.Add(-5 * time.Hour) },
			"schedule", "5h ago but the interval is 30m0s", "install-agent", Fail},
		{"low space", func(in *Input) { in.Free.Free = 20 * gib },
			"free space", "tier low", "bilgie auto", Warn},
		{"critical space", func(in *Input) { in.Free.Free = 2 * gib },
			"free space", "tier critical", "bilgie status", Fail},
		{"free unreadable", func(in *Input) { in.FreeErr = errors.New("statfs") },
			"free space", "cannot read free space: statfs", "mounted", Warn},
		{"shrinking fast", func(in *Input) {
			in.Runs = []auto.RunRecord{{Time: fixedNow.Add(-48 * time.Hour), Tier: "ok", FreeBefore: 260 * gib}}
			in.Free.Free = 200 * gib
		}, "trend", "-60 GiB over 2d", "bilgie status", Warn},
		{"stable trend", func(in *Input) {
			in.Runs = []auto.RunRecord{{Time: fixedNow.Add(-48 * time.Hour), Tier: "ok", FreeBefore: 205 * gib}}
		}, "trend", "-5.0 GiB over 2d", "", OK},
		{"disabled providers", func(in *Input) { in.Cfg = testConfig("gradle", "yarn") },
			"config", "2 provider(s) disabled: gradle, yarn", "", Note},
		{"protected conflict", func(in *Input) {
			in.Conflicts = []auto.Conflict{{Provider: "go", Reason: "protected path /home/u/Downloads/go"}}
		}, "config", "go: protected path /home/u/Downloads/go; auto never cleans it", "providers.go.enabled: false", Warn},
		{"notifier missing linux", func(in *Input) { in.NotifierErr = errors.New("not found") },
			"notifier", "notify-send not found", "libnotify", Warn},
		{"notifier missing windows", func(in *Input) {
			in.GOOS, in.NotifierProgram, in.NotifierErr = "windows", "powershell.exe", errors.New("nf")
		}, "notifier", "powershell.exe not found", "PATH", Warn},
		{"notifier ok", func(*Input) {}, "notifier", "notify-send is available", "", OK},
		{"first run pending", func(in *Input) { in.FirstRunPending = true },
			"first run", "still a dry-run", "", Note},
		{"corrupt log", func(in *Input) { in.Corrupt = 3 },
			"history", "3 unreadable line(s)", "", Note},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := healthy()
			tt.mutate(&in)
			f := find(t, Diagnose(in), tt.area)
			assert.Equal(t, tt.level, f.Level, f.Message)
			assert.Contains(t, f.Message, tt.contains)
			assert.Contains(t, f.Hint, tt.hint)
		})
	}
}

func TestDiagnose_AlwaysSkippedProvider(t *testing.T) {
	skip := func(reason string) []auto.ProviderRecord {
		return []auto.ProviderRecord{
			{Name: "gradle", Status: auto.StatusSkipped, Reason: reason},
			{Name: "go", Status: auto.StatusSkipped, Reason: "within limit"},
		}
	}
	run := func(age time.Duration, providers []auto.ProviderRecord) auto.RunRecord {
		return auto.RunRecord{Time: fixedNow.Add(-age), Tier: "low", FreeBefore: 200 * gib, Providers: providers}
	}

	t.Run("unavailable on every run", func(t *testing.T) {
		in := healthy()
		in.Runs = []auto.RunRecord{run(2*time.Hour, skip("unavailable")), run(time.Hour, skip("unavailable")), run(20*time.Minute, skip("unavailable"))}
		r := Diagnose(in)
		require.Equal(t, 1, r.Attention())
		var f Finding
		for _, c := range r.Findings {
			if c.Level == Warn {
				f = c
			}
		}
		assert.Equal(t, "gradle was skipped on each of the last 3 runs: unavailable", f.Message)
		assert.Contains(t, f.Hint, "enabled: false")
	})

	t.Run("recovered since", func(t *testing.T) {
		in := healthy()
		in.Runs = []auto.RunRecord{run(2*time.Hour, skip("unavailable")), run(20*time.Minute, []auto.ProviderRecord{{Name: "gradle", Status: auto.StatusCleaned}})}
		assert.Zero(t, Diagnose(in).Attention())
	})

	t.Run("within limit is routine", func(t *testing.T) {
		in := healthy()
		in.Runs = []auto.RunRecord{run(time.Hour, skip("within limit")), run(20*time.Minute, skip("within limit"))}
		assert.Zero(t, Diagnose(in).Attention())
	})

	t.Run("a protected conflict is not reported twice", func(t *testing.T) {
		in := healthy()
		reason := "protected path /x/Downloads"
		in.Runs = []auto.RunRecord{run(time.Hour, skip(reason)), run(20*time.Minute, skip(reason))}
		in.Conflicts = []auto.Conflict{{Provider: "gradle", Reason: reason}}
		assert.Equal(t, 1, Diagnose(in).Attention())
	})
}

func TestDiagnose_ConfigLoadFailure(t *testing.T) {
	in := healthy()
	in.Cfg, in.ConfigErr = nil, errors.New("validate config: bad")
	r := Diagnose(in)
	f := find(t, r, "config")
	assert.Equal(t, Fail, f.Level)
	assert.Contains(t, f.Message, "validate config: bad")
	assert.Positive(t, r.Attention())
}

func TestFreedCountsOnlyDeletingRunsInTheWindow(t *testing.T) {
	in := healthy()
	in.Runs = []auto.RunRecord{
		{Time: fixedNow.Add(-10 * 24 * time.Hour), FreedBytes: 50 * gib},
		{Time: fixedNow.Add(-3 * time.Hour), FreedBytes: 7 * gib, DryRun: true},
		{Time: fixedNow.Add(-2 * time.Hour), FreedBytes: 2 * gib},
		{Time: fixedNow.Add(-20 * time.Minute), FreedBytes: gib},
	}
	assert.Contains(t, find(t, Diagnose(in), "freed").Message, "3.0 GiB freed by 2 deleting run(s)")
}

func TestAge(t *testing.T) {
	assert.Equal(t, "under a minute", Age(10*time.Second))
	assert.Equal(t, "5m", Age(5*time.Minute))
	assert.Equal(t, "47h", Age(47*time.Hour))
	assert.Equal(t, "3d", Age(72*time.Hour))
	assert.Equal(t, "under a minute", Age(-time.Hour))
}
