package auto

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type slowDir struct{}

func (slowDir) ReadDir(int) ([]os.DirEntry, error) {
	time.Sleep(50 * time.Millisecond)
	return syntheticFiles(1), nil
}
func (slowDir) Close() error { return nil }

func TestFindGitMarker_OwnTimeoutIsNotTooLarge(t *testing.T) {
	old := openDir
	openDir = func(string) (dirReader, error) { return slowDir{}, nil }
	t.Cleanup(func() { openDir = old })
	lim := markerLimits
	lim.timeout = time.Millisecond

	res := findGitMarker(t.Context(), t.TempDir(), lim)

	assert.Equal(t, markerTimedOut, res.outcome)
}

func TestCheckProtected_TimeoutReasonSaysTimedOut(t *testing.T) {
	old := openDir
	openDir = func(string) (dirReader, error) { return slowDir{}, nil }
	t.Cleanup(func() { openDir = old })
	withMarkerLimits(t, scanLimits{timeout: time.Millisecond, depth: 3, dirs: 100, entries: 1000})
	root := t.TempDir()

	v := checkProtected(context.Background(), root, t.TempDir(), nil, true)

	assert.Equal(t, verdictUnverified, v.kind)
	assert.Equal(t, "scan timed out, cannot verify: "+root, v.detail)
	assert.NotContains(t, v.detail, "too large")
}

func TestHasGitMarker_ListingIsCappedAndMemoized(t *testing.T) {
	dir := t.TempDir()
	reads := 0
	old, oldCap := openAncestor, ancestorListCap
	openAncestor = func(string) (dirReader, error) {
		reads++
		return &fakeDir{entries: syntheticFiles(10 * listBatch)}, nil
	}
	ancestorListCap = listBatch
	t.Cleanup(func() { openAncestor, ancestorListCap = old, oldCap })

	assert.False(t, hasGitMarker(dir))
	assert.False(t, hasGitMarker(dir))

	assert.Equal(t, 1, reads, "second check reuses the first answer")
}

func TestHasGitMarker_CapStopsListingEarly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	batches := 0
	old, oldCap := openAncestor, ancestorListCap
	openAncestor = func(string) (dirReader, error) {
		return &countingDir{batches: &batches}, nil
	}
	ancestorListCap = 3 * listBatch
	t.Cleanup(func() { openAncestor, ancestorListCap = old, oldCap })

	assert.False(t, hasGitMarker(dir))
	assert.Equal(t, 3, batches)
}

type countingDir struct{ batches *int }

func (d *countingDir) ReadDir(int) ([]os.DirEntry, error) {
	*d.batches++
	out := make([]os.DirEntry, listBatch)
	for i := range out {
		out[i] = fakeEntry{name: fmt.Sprintf("f%d-%d", *d.batches, i)}
	}
	return out, nil
}
func (d *countingDir) Close() error { return nil }

func TestProtectionConflicts_CancelledContextReportsIncomplete(t *testing.T) {
	h := newHarness(t)
	root := h.dir("cache")
	h.add("cache", true, true, func(f *fakeProvider, pc *config.Provider) {
		pc.Paths = []string{root}
		f.paths = []string{root}
	})
	newProvider := func(name string, _ config.Provider) (provider.Provider, error) { return h.fakes[name], nil }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := ProtectionConflicts(ctx, h.cfg, h.home, newProvider)

	require.Len(t, got, 1)
	assert.True(t, got[0].Incomplete)
	assert.Equal(t, "cache", got[0].Provider)
}

func TestRun_SweepSkipsOnlyTheProtectedMatch(t *testing.T) {
	home := sandboxHome(t)
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Auto:      autoCfg(),
		Protected: []string{"~/scratch/sweep-keep"},
	}
	keep := filepath.Join(home, "scratch", "sweep-keep", "a.bin")
	gone := filepath.Join(home, "scratch", "sweep-gone", "a.bin")
	writeAged(t, keep)
	writeAged(t, gone)
	noOpenCheck := false
	cfg.Providers["sweep"] = config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(home, "scratch", "sweep-*")},
		MinIdle: "1h", SkipIfOpen: &noOpenCheck, MaxSize: "1B",
	}
	deps := Deps{
		Free:        func() (FreeSpace, error) { return FreeSpace{Free: 1 * gib, Total: 1000 * gib}, nil },
		NewProvider: provider.NewProvider,
		Out:         &bytes.Buffer{},
		Home:        home,
	}

	report, err := Run(t.Context(), cfg, false, deps)

	require.NoError(t, err)
	assert.FileExists(t, keep)
	assert.NoFileExists(t, gone, "the clean match must still go")
	require.Len(t, report.Results, 1)
	assert.Equal(t, StatusCleaned, report.Results[0].Status)
	assert.Equal(t, 1, report.Results[0].SkippedEntries)
}

func TestRun_SweepWithEveryMatchProtectedIsSkipped(t *testing.T) {
	home := sandboxHome(t)
	cfg := &config.Config{
		Providers: map[string]config.Provider{},
		Auto:      autoCfg(),
		Protected: []string{"~/scratch"},
	}
	keep := filepath.Join(home, "scratch", "sweep-keep", "a.bin")
	writeAged(t, keep)
	noOpenCheck := false
	cfg.Providers["sweep"] = config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(home, "scratch", "sweep-*")},
		MinIdle: "1h", SkipIfOpen: &noOpenCheck, MaxSize: "1B",
	}
	deps := Deps{
		Free:        func() (FreeSpace, error) { return FreeSpace{Free: 1 * gib, Total: 1000 * gib}, nil },
		NewProvider: provider.NewProvider,
		Out:         &bytes.Buffer{},
		Home:        home,
	}

	report, err := Run(t.Context(), cfg, false, deps)

	require.NoError(t, err)
	assert.FileExists(t, keep)
	require.Len(t, report.Results, 1)
	assert.Equal(t, StatusSkipped, report.Results[0].Status)
	assert.Contains(t, report.Results[0].Reason, "protected path")
}

func TestHasGitMarker_FailedListingIsNotMemoized(t *testing.T) {
	dir := t.TempDir()
	opens := 0
	old := openAncestor
	openAncestor = func(string) (dirReader, error) {
		opens++
		return nil, os.ErrPermission
	}
	t.Cleanup(func() { openAncestor = old })

	assert.False(t, hasGitMarker(dir))
	assert.False(t, hasGitMarker(dir))

	assert.Equal(t, 2, opens)
}
