package osshim

import (
	"errors"
	"os"
	"path/filepath"
)

// LockHeld reports whether another holder has an exclusive lock on path. A
// missing file means nobody holds it. The probe locks through its own
// descriptor and drops it on return, so it never leaves a lock behind.
func LockHeld(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	acquired, err := lockFile(f)
	if err != nil {
		return false, err
	}
	return !acquired, nil
}

// TryLock creates path if needed and takes an exclusive lock on it without
// blocking. When acquired is false another holder owns the lock. Call release
// once acquired is true.
func TryLock(path string) (release func(), acquired bool, err error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	acquired, err = lockFile(f)
	if err != nil || !acquired {
		_ = f.Close()
		return nil, false, err
	}
	return func() { _ = f.Close() }, true, nil
}
