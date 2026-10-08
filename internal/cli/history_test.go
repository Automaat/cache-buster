package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/auto"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noteLog struct{ titles []string }

func (n *noteLog) notifier(err error) auto.Notifier {
	return func(_ context.Context, title, _ string) error {
		n.titles = append(n.titles, title)
		return err
	}
}

func readRecords(t *testing.T, f *autoFixture) []auto.RunRecord {
	t.Helper()
	runs, _, err := auto.ReadRuns(f.env.stateDir, 0)
	require.NoError(t, err)
	return runs
}

func TestAuto_AppendsOneRecordPerRunAndHistoryReadsItBack(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Equal(t, "low", runs[0].Tier)
	assert.Equal(t, 100*autoGiB, runs[0].FreeBefore)
	require.Len(t, runs[0].Providers, 1)
	assert.Equal(t, "tool", runs[0].Providers[0].Name)
	assert.Equal(t, auto.StatusCleaned, runs[0].Providers[0].Status)

	var out bytes.Buffer
	require.NoError(t, runHistoryIn(&out, f.env.stateDir, 10, false))
	assert.Contains(t, out.String(), "low")
	assert.Contains(t, out.String(), "TIME")
}

func TestAuto_RecordsSkippedProvidersWithReasons(t *testing.T) {
	f := newAutoFixture(t, 500*autoGiB, "")

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Equal(t, "ok", runs[0].Tier)
	assert.Equal(t, auto.StatusSkipped, runs[0].Providers[0].Status)
	assert.Equal(t, "within limit", runs[0].Providers[0].Reason)
}

func TestAuto_NotifiesOnlyWhenStillUnderThreshold(t *testing.T) {
	t.Run("space stays low", func(t *testing.T) {
		f := newAutoFixture(t, 100*autoGiB, "")
		var notes noteLog
		f.env.notify = notes.notifier(nil)

		require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

		assert.Equal(t, []string{"Disk space is low"}, notes.titles)
		assert.True(t, readRecords(t, f)[0].Notified)
	})

	t.Run("cleanup recovers space", func(t *testing.T) {
		f := newAutoFixture(t, 100*autoGiB, "")
		var notes noteLog
		f.env.notify = notes.notifier(nil)
		f.env.free = func() (auto.FreeSpace, error) {
			if f.cleaned() {
				return auto.FreeSpace{Free: 400 * autoGiB, Total: 1000 * autoGiB}, nil
			}
			return auto.FreeSpace{Free: 100 * autoGiB, Total: 1000 * autoGiB}, nil
		}

		require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

		assert.Empty(t, notes.titles)
		assert.False(t, readRecords(t, f)[0].Notified)
	})

	t.Run("plenty of space", func(t *testing.T) {
		f := newAutoFixture(t, 500*autoGiB, "")
		var notes noteLog
		f.env.notify = notes.notifier(nil)

		require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

		assert.Empty(t, notes.titles)
	})

	t.Run("dry-run never notifies", func(t *testing.T) {
		f := newAutoFixture(t, 1*autoGiB, "")
		var notes noteLog
		f.env.notify = notes.notifier(nil)

		require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, true))

		assert.Empty(t, notes.titles)
		assert.True(t, readRecords(t, f)[0].DryRun)
	})
}

func TestAuto_NotifierFailureDoesNotFailTheRun(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	var notes noteLog
	f.env.notify = notes.notifier(errors.New("no permission"))

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.Contains(t, f.out.String(), "warning: send notification")
	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.False(t, runs[0].Notified)
}

func TestAuto_RecordFailureDoesNotFailTheRun(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	require.NoError(t, os.MkdirAll(filepath.Join(f.env.stateDir, auto.RunLogName), 0o750))

	require.NoError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	assert.Contains(t, f.out.String(), "warning: record run")
	assert.True(t, f.cleaned())
}

func TestAuto_ProviderErrorStillRecorded(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	f.env.newProvider = func(string, config.Provider) (provider.Provider, error) { return nil, os.ErrInvalid }

	require.Error(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Equal(t, auto.StatusError, runs[0].Providers[0].Status)
	assert.NotEmpty(t, runs[0].Providers[0].Error)
}

func TestHistory_Output(t *testing.T) {
	dir := t.TempDir()
	rep := auto.Report{
		Tier: auto.TierLow, Start: auto.FreeSpace{Free: 10 * autoGiB, Total: 500 * autoGiB},
		End:     auto.FreeSpace{Free: 12 * autoGiB, Total: 500 * autoGiB},
		Results: []auto.Result{{Name: "npm", Status: auto.StatusCleaned, Freed: 2 * autoGiB}},
	}
	require.NoError(t, auto.AppendRun(dir, auto.NewRunRecord(rep, time.Now().Add(-time.Hour), nil, false)))
	require.NoError(t, auto.AppendRun(dir, auto.NewRunRecord(rep, time.Now(), nil, true)))
	f, err := os.OpenFile(filepath.Join(dir, auto.RunLogName), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("garbage\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	t.Run("table tolerates corrupt line and honors limit", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, runHistoryIn(&out, dir, 1, false))
		assert.Contains(t, out.String(), "notified")
		assert.Contains(t, out.String(), "1 unreadable log line(s) skipped")
	})

	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, runHistoryIn(&out, dir, 0, true))
		var got []auto.RunRecord
		require.NoError(t, json.Unmarshal(out.Bytes(), &got))
		assert.Len(t, got, 2)
	})

	t.Run("empty", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, runHistoryIn(&out, t.TempDir(), 5, false))
		assert.Contains(t, out.String(), "No auto runs recorded")

		out.Reset()
		require.NoError(t, runHistoryIn(&out, t.TempDir(), 5, true))
		assert.JSONEq(t, "[]", out.String())
	})

	t.Run("negative limit", func(t *testing.T) {
		require.Error(t, runHistoryIn(&bytes.Buffer{}, dir, -1, false))
	})
}

func TestStatus_ListsUnmanagedDirsFromInjectedScan(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	cfgPath := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: \"1\"\nproviders:\n  tool:\n    enabled: true\n    paths:\n      - "+cacheDir+"\n    max_size: 1G\n"), 0o600))
	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	var coveredSeen []string
	scan := func(_ context.Context, cfg *config.Config) auto.UnmanagedReport {
		coveredSeen = auto.CoveredPaths(cfg)
		return auto.UnmanagedReport{Dirs: []auto.UnmanagedDir{{Path: "/x/y", Bytes: 3 * autoGiB}}, Incomplete: true}
	}

	var err error
	out := captureStdout(t, func() { err = runStatusWithLoader(loader, false, scan, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, "/x/y")
	assert.Contains(t, out, "3.0 GiB")
	assert.Contains(t, out, "time budget")
	assert.Equal(t, []string{cacheDir}, coveredSeen)

	jsonOut := captureStdout(t, func() { err = runStatusWithLoader(loader, true, scan, nil) })
	require.NoError(t, err)
	var parsed struct {
		Unmanaged struct {
			Dirs []struct {
				Path  string `json:"path"`
				Bytes int64  `json:"bytes"`
			} `json:"dirs"`
			Incomplete bool `json:"incomplete"`
		} `json:"unmanaged"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &parsed))
	require.Len(t, parsed.Unmanaged.Dirs, 1)
	assert.Equal(t, "/x/y", parsed.Unmanaged.Dirs[0].Path)
	assert.True(t, parsed.Unmanaged.Incomplete)

	noScan := captureStdout(t, func() { err = runStatusWithLoader(loader, true, nil, nil) })
	require.NoError(t, err)
	assert.NotContains(t, noScan, "unmanaged")
}

func TestAuto_FreeSpaceReadFailureIsStillRecorded(t *testing.T) {
	f := newAutoFixture(t, 100*autoGiB, "")
	f.env.free = func() (auto.FreeSpace, error) { return auto.FreeSpace{}, os.ErrInvalid }

	require.Error(t, runAutoWithLoader(t.Context(), f.loader, f.env, false))

	runs := readRecords(t, f)
	require.Len(t, runs, 1)
	assert.Contains(t, runs[0].Error, "read free space")
}

func TestRunHistory_ReadsStateDirUnderHome(t *testing.T) {
	skipOnWindows(t, "#206 per-OS paths and permissions")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg"))
	stateDir, err := auto.StateDir()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(stateDir, home))
	require.NoError(t, auto.AppendRun(stateDir, auto.NewRunRecord(auto.Report{Tier: auto.TierLow}, time.Now(), nil, false)))

	cmd := &cobra.Command{}
	cmd.Flags().Int("limit", 10, "")
	cmd.Flags().Bool("json", false, "")

	var runErr error
	out := captureStdout(t, func() { runErr = runHistory(cmd, nil) })

	require.NoError(t, runErr)
	assert.Contains(t, out, "low")
}

func TestHistoryNotes(t *testing.T) {
	notes := historyNotes(auto.RunRecord{DryRun: true, Recovered: true, Notified: true, Error: "x"})

	assert.Equal(t, "dry-run, recovered, notified, run error", notes)
}

func TestStatus_ListsProtectedEntriesFromInjectedScan(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	cfgPath := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: \"1\"\nproviders:\n  tool:\n    enabled: true\n    paths:\n      - "+cacheDir+"\n    max_size: 1G\n"), 0o600))
	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	protect := func(_ context.Context, _ *config.Config) auto.ProtectedReport {
		return auto.ProtectedReport{
			Entries:    []auto.ProtectedEntry{{Path: "/p/Downloads", Bytes: 5 * autoGiB, Partial: true}},
			Incomplete: true,
		}
	}

	var err error
	out := captureStdout(t, func() { err = runStatusWithLoader(loader, false, nil, protect) })
	require.NoError(t, err)
	assert.Contains(t, out, "Needs a human")
	assert.Contains(t, out, "/p/Downloads")
	assert.Contains(t, out, "5.0 GiB")
	assert.Contains(t, out, "at least")

	jsonOut := captureStdout(t, func() { err = runStatusWithLoader(loader, true, nil, protect) })
	require.NoError(t, err)
	var parsed struct {
		Protected struct {
			Entries []struct {
				Path  string `json:"path"`
				Bytes int64  `json:"bytes"`
			} `json:"entries"`
			Incomplete bool `json:"incomplete"`
		} `json:"protected"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &parsed))
	require.Len(t, parsed.Protected.Entries, 1)
	assert.Equal(t, "/p/Downloads", parsed.Protected.Entries[0].Path)
	assert.Equal(t, 5*autoGiB, parsed.Protected.Entries[0].Bytes)
	assert.True(t, parsed.Protected.Incomplete)

	none := captureStdout(t, func() { err = runStatusWithLoader(loader, true, nil, nil) })
	require.NoError(t, err)
	assert.NotContains(t, none, "protected")
}

func TestStatus_NoEnabledProvidersStillListsProtectedEntries(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: \"1\"\nproviders: {}\n"), 0o600))
	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	protect := func(_ context.Context, _ *config.Config) auto.ProtectedReport {
		return auto.ProtectedReport{Entries: []auto.ProtectedEntry{{Path: "/p/Downloads", Bytes: autoGiB}}}
	}

	var err error
	out := captureStdout(t, func() { err = runStatusWithLoader(loader, false, nil, protect) })

	require.NoError(t, err)
	assert.Contains(t, out, "No enabled providers")
	assert.Contains(t, out, "/p/Downloads")
}
