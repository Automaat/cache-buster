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
	for _, pid := range descendants(root, procs) {
		if err := terminatePID(pid); err != nil {
			errs = append(errs, err)
		}
	}

	if err := killRoot(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func terminatePID(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.TerminateProcess(h, 1)
}
