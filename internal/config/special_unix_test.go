//go:build !windows

package config

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_FifoAsTheConfigFailsInsteadOfBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	l := NewLoader()
	l.SetConfigPath(path)
	done := make(chan error, 1)

	go func() { _, err := l.Load(); done <- err }()

	select {
	case err := <-done:
		assert.ErrorContains(t, err, "not a regular file")
	case <-time.After(2 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}
