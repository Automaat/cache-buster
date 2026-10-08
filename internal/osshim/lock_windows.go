//go:build windows

package osshim

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// LockHeld reports whether another handle holds a lock on path, using a
// non-blocking exclusive LockFileEx. A missing file means nobody holds it.
func LockHeld(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err = windows.LockFileEx(h, flags, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, windows.UnlockFileEx(h, 0, 1, 0, ol)
}

// HoldLock takes an exclusive lock on path and returns its release func. It
// simulates a tool holding its cache lock, for tests.
func HoldLock(path string) (release func(), err error) {
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
