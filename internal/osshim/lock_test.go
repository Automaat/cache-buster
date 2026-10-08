package osshim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockHeld(t *testing.T) {
	t.Run("missing file is not held", func(t *testing.T) {
		held, err := LockHeld(filepath.Join(t.TempDir(), ".lock"))
		require.NoError(t, err)
		assert.False(t, held)
	})

	t.Run("unlocked file is not held and stays lockable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".lock")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		held, err := LockHeld(path)
		require.NoError(t, err)
		assert.False(t, held)

		held, err = LockHeld(path)
		require.NoError(t, err)
		assert.False(t, held, "probe must release its lock")
	})

	t.Run("lock held by another descriptor", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".lock")
		release, acquired, err := TryLock(path)
		require.NoError(t, err)
		require.True(t, acquired)

		held, err := LockHeld(path)
		require.NoError(t, err)
		assert.True(t, held)

		release()
		held, err = LockHeld(path)
		require.NoError(t, err)
		assert.False(t, held, "released lock is free again")
	})
}

func TestTryLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	release, acquired, err := TryLock(path)
	require.NoError(t, err)
	require.True(t, acquired)

	_, second, err := TryLock(path)
	require.NoError(t, err)
	assert.False(t, second, "second holder must be refused")

	release()
	release, again, err := TryLock(path)
	require.NoError(t, err)
	assert.True(t, again, "lock is free after release")
	release()
}
