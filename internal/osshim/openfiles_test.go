package osshim

import (
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
	require.NoError(t, err)
	assert.False(t, open, "no process holds a file yet")

	f, err := os.Open(held)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	open, err = HasOpenFiles(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, open)
}
