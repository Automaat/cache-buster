//go:build windows

package osshim

import (
	"errors"
	"math"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// KillTreeOnCancel makes cancelling cmd's context terminate the command and
// every descendant found in a process snapshot, so grandchildren die with it.
// It must run before Start.
func KillTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		if pid <= 0 || pid > math.MaxInt32 {
			return cmd.Process.Kill()
		}
		return killTree(uint32(pid), cmd.Process.Kill)
	}
}

func killTree(root uint32, killRoot func() error) error {
	entries, snapErr := snapshotProcesses()
	var errs []error
	if snapErr != nil {
		errs = append(errs, snapErr)
	}

	procs := make([]procInfo, len(entries))
	for i := range entries {
		procs[i] = entries[i].procInfo
	}
	rootStart, rootErr := creationTime(root)
	if rootErr != nil {
		errs = append(errs, rootErr)
	}
	for _, pid := range descendants(root, procs) {
		if rootErr != nil {
			break
		}
		if err := terminateIfStartedAfter(pid, rootStart); err != nil {
			errs = append(errs, err)
		}
	}

	if err := killRoot(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// terminateIfStartedAfter kills pid through a single handle once its start
// time proves it is no older than the tree root, so a recycled id is never hit.
// A process that already exited is not an error.
func terminateIfStartedAfter(pid uint32, rootStart int64) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	start, err := handleCreationTime(h)
	if err != nil {
		return err
	}
	if start < rootStart {
		return nil
	}
	return windows.TerminateProcess(h, 1)
}

// creationTime returns a process start time in 100ns ticks. A descendant
// cannot predate its ancestor, so an earlier start exposes a stale parent id
// that Windows has since reused.
func creationTime(pid uint32) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return handleCreationTime(h)
}

func handleCreationTime(h windows.Handle) (int64, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return created.Nanoseconds(), nil
}
