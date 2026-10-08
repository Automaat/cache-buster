package osshim

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasOpenFiles(t *testing.T) {
	dir := t.TempDir()
	held := filepath.Join(dir, "held.txt")
	require.NoError(t, os.WriteFile(held, []byte("x"), 0o600))

	if runtime.GOOS == "windows" {
		open, err := HasOpenFiles(t.Context(), dir)
		require.ErrorIs(t, err, ErrOpenFilesUnsupported, "windows must fail closed")
		assert.False(t, open)
		return
	}
	if runtime.GOOS != "linux" {
		if _, err := exec.LookPath("lsof"); err != nil {
			t.Skip("lsof not installed")
		}
	}

	open, err := HasOpenFiles(t.Context(), dir)
	if errors.Is(err, os.ErrPermission) {
		t.Skip("host has same-user processes whose files cannot be inspected; the check fails closed")
	}
	require.NoError(t, err)
	assert.False(t, open, "no process holds a file yet")

	f, err := os.Open(held)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	open, err = HasOpenFiles(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, open)
}

func TestOpenFilesUnder(t *testing.T) {
	busy, idle := t.TempDir(), t.TempDir()
	held := filepath.Join(busy, "held.txt")
	require.NoError(t, os.WriteFile(held, []byte("x"), 0o600))
	missing := filepath.Join(t.TempDir(), "gone")

	if runtime.GOOS == "windows" {
		_, err := OpenFilesUnder(t.Context(), []string{busy})
		require.ErrorIs(t, err, ErrOpenFilesUnsupported, "windows must fail closed")
		return
	}
	if runtime.GOOS != "linux" {
		if _, err := exec.LookPath("lsof"); err != nil {
			t.Skip("lsof not installed")
		}
	}

	f, err := os.Open(held)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	open, err := OpenFilesUnder(t.Context(), []string{busy, idle, missing})
	if errors.Is(err, os.ErrPermission) {
		t.Skip("host has same-user processes whose files cannot be inspected; the check fails closed")
	}
	require.NoError(t, err)
	assert.True(t, open[busy])
	assert.False(t, open[idle])
	assert.False(t, open[missing])
}
