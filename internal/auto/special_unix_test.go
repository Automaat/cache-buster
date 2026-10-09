//go:build !windows

package auto

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRuns_FifoAsTheRunLogFailsInsteadOfBlocking(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, RunLogName), 0o600))
	done := make(chan error, 1)

	go func() { _, _, err := ReadRuns(dir, 0); done <- err }()

	select {
	case err := <-done:
		assert.ErrorContains(t, err, "not a regular file")
	case <-time.After(2 * time.Second):
		t.Fatal("ReadRuns blocked on a FIFO")
	}
}
