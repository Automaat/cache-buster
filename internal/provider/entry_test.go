package provider

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

func makeEntry(t *testing.T, root, name string, bytes int, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "f"), make([]byte, bytes), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(dir, when, when))
	return dir
}

func newEntryProvider(t *testing.T, root, maxSize, maxAge string) *EntryProvider {
	t.Helper()
	p, err := NewEntryProvider("test", config.Provider{Paths: []string{root}, MaxSize: maxSize, MaxAge: maxAge, Enabled: true})
	require.NoError(t, err)
	return p
}

func TestEntryProvider_RemovesWholeOldestEntries(t *testing.T) {
	root := t.TempDir()
	old := makeEntry(t, root, "old", 2048, 48*time.Hour)
	mid := makeEntry(t, root, "mid", 2048, 24*time.Hour)
	fresh := makeEntry(t, root, "fresh", 2048, time.Hour)

	res, err := newEntryProvider(t, root, "4K", "").Clean(context.Background(), CleanOptions{})
	require.NoError(t, err)

	assert.NoDirExists(t, old)
	assert.DirExists(t, mid)
	assert.DirExists(t, fresh)
	assert.FileExists(t, filepath.Join(mid, "sub", "f"))
	assert.Equal(t, int64(2048), res.BytesCleaned)
}

func TestEntryProvider_UnderLimitKeepsAll(t *testing.T) {
	root := t.TempDir()
	e := makeEntry(t, root, "a", 10, time.Hour)

	res, err := newEntryProvider(t, root, "1G", "").Clean(context.Background(), CleanOptions{})
	require.NoError(t, err)
	assert.DirExists(t, e)
	assert.Equal(t, "already under limit", res.Output)
}

func TestEntryProvider_DryRunKeepsFiles(t *testing.T) {
	root := t.TempDir()
	e := makeEntry(t, root, "a", 2048, time.Hour)

	res, err := newEntryProvider(t, root, "1K", "").Clean(context.Background(), CleanOptions{DryRun: true})
	require.NoError(t, err)
	assert.DirExists(t, e)
	assert.Contains(t, res.Output, "would remove")
}

func TestEntryProvider_SmartIgnoresMaxAge(t *testing.T) {
	root := t.TempDir()
	old := makeEntry(t, root, "old", 10, 100*24*time.Hour)

	_, err := newEntryProvider(t, root, "1G", "30d").Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)
	assert.DirExists(t, old)
}

func TestEntryProvider_AgesByNewestFile(t *testing.T) {
	root := t.TempDir()
	// Old directory mtime, but a file written inside recently.
	busy := makeEntry(t, root, "busy", 2048, 100*24*time.Hour)
	file := filepath.Join(busy, "sub", "f")
	require.NoError(t, os.Chtimes(file, time.Now(), time.Now()))
	idle := makeEntry(t, root, "idle", 2048, 24*time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(idle, "sub", "f"), time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour)))

	_, err := newEntryProvider(t, root, "2K", "").Clean(context.Background(), CleanOptions{})
	require.NoError(t, err)
	assert.DirExists(t, busy)
	assert.NoDirExists(t, idle)
}

func TestEntryProvider_MissingPath(t *testing.T) {
	res, err := newEntryProvider(t, filepath.Join(t.TempDir(), "nope"), "1K", "").Clean(context.Background(), CleanOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.BytesCleaned)
}
