//go:build !windows

package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func within(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatal("blocked on a special file")
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, syscall.Mkfifo(path, 0o600))
}

func TestMarker_FifoNeverBlocksAndHeals(t *testing.T) {
	home := t.TempDir()
	mkfifo(t, markerPath(home))

	within(t, 2*time.Second, func() {
		assert.True(t, ShouldReport(home, "k", "sig"))
		Resolved(home, "other")
	})

	assert.False(t, ShouldReport(home, "k", "sig"), "the marker was replaced by a regular file")
}

func TestLegacy_FifoAtTheLockPathStillMigrates(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "x")
	mkfifo(t, filepath.Join(home, ".cache", "bilgie", "migration.lock"))

	within(t, 2*migrateWait+time.Second, func() { Legacy(home, &bytes.Buffer{}) })

	assert.FileExists(t, filepath.Join(dirs[0][1], "config.yaml"))
}

func TestPendingConfig_FifoAsTheNewConfigIsAbsent(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "real: 1")
	mkfifo(t, filepath.Join(dirs[0][1], "config.yaml"))

	var p *Pending
	within(t, 2*time.Second, func() { p = PendingConfig(home) })

	require.NotNil(t, p)
	assert.NotPanics(t, func() { _ = p.Step() })
}

func TestPendingConfig_FifoAsTheLegacyConfigDoesNotBlock(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	mkfifo(t, filepath.Join(dirs[0][0], "config.yaml"))

	within(t, 2*time.Second, func() { _ = PendingConfig(home) })
}
