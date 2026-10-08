package auto

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkdirFile(t *testing.T, path string, n int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, make([]byte, n), 0o600))
}

func TestScanUnmanaged_ReportsOnlyUncoveredDirsLargestFirst(t *testing.T) {
	root := t.TempDir()
	mkdirFile(t, filepath.Join(root, "big", "a.bin"), 300000)
	mkdirFile(t, filepath.Join(root, "small", "a.bin"), 20000)
	mkdirFile(t, filepath.Join(root, "managed", "a.bin"), 900000)
	mkdirFile(t, filepath.Join(root, "parent", "child", "a.bin"), 900000)
	mkdirFile(t, filepath.Join(root, "tiny", "a.bin"), 10)
	mkdirFile(t, filepath.Join(root, "loose.bin"), 900000)

	report := ScanUnmanaged(t.Context(), ScanOptions{
		Roots:    []string{root},
		Covered:  []string{filepath.Join(root, "managed"), filepath.Join(root, "parent", "child")},
		Top:      5,
		MinBytes: 8192,
	})

	var paths []string
	for _, d := range report.Dirs {
		paths = append(paths, filepath.Base(d.Path))
	}
	assert.Equal(t, []string{"big", "small"}, paths)
	assert.False(t, report.Incomplete)
	assert.GreaterOrEqual(t, report.Dirs[0].Bytes, int64(300000))
}

func TestScanUnmanaged_CoverageFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "real")
	mkdirFile(t, filepath.Join(target, "a.bin"), 100000)
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link")))
	mkdirFile(t, filepath.Join(root, "plain", "a.bin"), 100000)

	report := ScanUnmanaged(t.Context(), ScanOptions{Roots: []string{root}, Covered: []string{target}, MinBytes: 1})

	require.Len(t, report.Dirs, 1)
	assert.Equal(t, "plain", filepath.Base(report.Dirs[0].Path))
}

func TestScanUnmanaged_TopLimitAndRootDedupe(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		mkdirFile(t, filepath.Join(root, n, "f"), 50000)
	}

	report := ScanUnmanaged(t.Context(), ScanOptions{Roots: []string{root, root}, Top: 2, MinBytes: 1})

	assert.Len(t, report.Dirs, 2)
}

func TestScanUnmanaged_CancelledContextStopsAndFlagsIncomplete(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d"} {
		mkdirFile(t, filepath.Join(root, n, "f"), 50000)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	report := ScanUnmanaged(ctx, ScanOptions{Roots: []string{root}, MinBytes: 1})

	assert.True(t, report.Incomplete)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestScanUnmanaged_BudgetBoundsScan(t *testing.T) {
	root := t.TempDir()
	for i := range 200 {
		mkdirFile(t, filepath.Join(root, "d", string(rune('a'+i%26)), string(rune('A'+i/26)), "f"), 10)
	}

	start := time.Now()
	report := ScanUnmanaged(t.Context(), ScanOptions{Roots: []string{root}, MinBytes: 0, Budget: time.Nanosecond})

	assert.True(t, report.Incomplete)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestScanUnmanaged_MissingRootIsSkipped(t *testing.T) {
	report := ScanUnmanaged(t.Context(), ScanOptions{Roots: []string{filepath.Join(t.TempDir(), "absent")}})

	assert.Empty(t, report.Dirs)
	assert.False(t, report.Incomplete)
}

func TestCoveredPaths_EnabledAndDirPatternSweepsOnly(t *testing.T) {
	dir := t.TempDir()
	on, off, sweep := filepath.Join(dir, "on"), filepath.Join(dir, "off"), filepath.Join(dir, "sail-1")
	for _, d := range []string{on, off, sweep} {
		require.NoError(t, os.MkdirAll(d, 0o750))
	}
	cfg := &config.Config{Providers: map[string]config.Provider{
		"on":    {Enabled: true, Paths: []string{on}},
		"off":   {Enabled: false, Paths: []string{off}},
		"sweep": {Enabled: false, Type: config.TypeDirPattern, Paths: []string{filepath.Join(dir, "sail*")}},
	}}

	assert.ElementsMatch(t, []string{on, sweep}, CoveredPaths(cfg))
}

func TestUnmanagedRoots_CoversSharedCachesAndTemp(t *testing.T) {
	home := t.TempDir()

	roots := UnmanagedRoots(home)

	assert.Contains(t, roots, filepath.Join(home, ".local", "share"))
	assert.Contains(t, roots, os.TempDir())
}
