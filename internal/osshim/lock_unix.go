//go:build unix

package osshim

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// LockHeld reports whether another descriptor holds an flock on path, using a
// non-blocking try-lock. A missing file means nobody holds it.
func LockHeld(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	fd := int(f.Fd())
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, unix.Flock(fd, unix.LOCK_UN)
}

// HoldLock takes an exclusive lock on path and returns its release func. It
// simulates a tool holding its cache lock, for tests.
func HoldLock(path string) (release func(), err error) {
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
