package auto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	stateDirName    = "cache-buster"
	firstRunMarker  = "first-run-pending"
	runLockFileName = "auto.lock"
)

// StateDir returns ~/.local/state/cache-buster. It ignores XDG_STATE_HOME on
// purpose: install-agent runs in a shell and the agent under launchd, and both
// must find the same marker.
func StateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", stateDirName), nil
}

// MarkFirstRunPending records that the next auto run must be a dry-run.
func MarkFirstRunPending(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, firstRunMarker), []byte("dry-run pending\n"), 0o600); err != nil {
		return fmt.Errorf("write first-run marker: %w", err)
	}
	return nil
}

// FirstRunPending reports whether the next auto run must be a dry-run. A
// marker that cannot be read counts as pending: the safe answer is no deletion.
func FirstRunPending(stateDir string) bool {
	_, err := os.Stat(filepath.Join(stateDir, firstRunMarker))
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

// ClearFirstRun removes the marker once the dry-run has happened.
func ClearFirstRun(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, firstRunMarker))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove first-run marker: %w", err)
	}
	return nil
}

// RunLock is an exclusive lock that keeps two auto runs from overlapping.
type RunLock struct {
	f *os.File
}

// AcquireRunLock takes the lock without blocking. The bool is false when
// another run holds it.
func AcquireRunLock(stateDir string) (*RunLock, bool, error) {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, false, fmt.Errorf("create state dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(stateDir, runLockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open run lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock run lock: %w", err)
	}
	return &RunLock{f: f}, true, nil
}

// Release drops the lock.
func (l *RunLock) Release() {
	_ = l.f.Close()
}
