//go:build !windows

package fsx

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func within(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a special file")
	}
}

func TestReadRegular_NeverBlocksOnSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	sock := filepath.Join(os.TempDir(), "fsx-"+filepath.Base(dir)+".sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close(); _ = os.Remove(sock) })
	link := filepath.Join(dir, "link-to-fifo")
	require.NoError(t, os.Symlink(fifo, link))

	for name, path := range map[string]string{"fifo": fifo, "socket": sock, "link to fifo": link, "device": "/dev/null"} {
		within(t, func() {
			_, err := ReadRegular(path, 10)
			assert.ErrorIs(t, err, ErrNotRegular, name)
			assert.ErrorIs(t, CheckRegular(path), ErrNotRegular, name)
		})
	}
}
