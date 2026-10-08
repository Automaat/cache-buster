//go:build unix

package osshim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pidDirs lists the numeric entries of a /proc-style root. An empty result
// means the root is not a real process table, so the caller must not read it
// as "nothing is running".
func pidDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var pids []string
	for _, e := range entries {
		if _, convErr := strconv.Atoi(e.Name()); convErr == nil && e.IsDir() {
			pids = append(pids, e.Name())
		}
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("no processes found under %s", root)
	}
	return pids, nil
}

// commandLinesFromProc returns one command line per process under a
// /proc-style root. The kernel's short name is appended so a process that
// rewrote its argv is still matched; a kernel thread or zombie has no argv
// and yields the name alone.
func commandLinesFromProc(ctx context.Context, root string) ([]string, error) {
	pids, err := pidDirs(root)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0, len(pids))
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cmdline, err := os.ReadFile(filepath.Join(root, pid, "cmdline"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		comm, err := os.ReadFile(filepath.Join(root, pid, "comm"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		args := strings.TrimSpace(strings.ReplaceAll(string(bytes.TrimRight(cmdline, "\x00")), "\x00", " "))
		name := strings.TrimSpace(string(comm))
		lines = append(lines, strings.TrimSpace(args+" "+name))
	}
	return lines, nil
}

// underDir reports whether path is dir or lies inside it.
func underDir(path, dir string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// procHasOpenFiles reports whether any process under a /proc-style root has a
// file open, a mapping, its working directory, root or executable inside dir.
// A process owned by another user that cannot be inspected is skipped, since
// a cache owned by selfUID is not theirs to hold; an uninspectable process
// of our own user is an error so the caller fails closed.
func procHasOpenFiles(ctx context.Context, root, dir string, selfUID int) (bool, error) {
	pids, err := pidDirs(root)
	if err != nil {
		return false, err
	}

	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		base := filepath.Join(root, pid)
		info, err := os.Stat(base)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		other := false
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			other = int(st.Uid) != selfUID
		}

		open, err := procPidHasOpenFiles(base, dir)
		switch {
		case err == nil && open:
			return true, nil
		case err == nil, errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ESRCH):
			continue
		case other && errors.Is(err, os.ErrPermission):
			continue
		default:
			return false, fmt.Errorf("inspect process %s: %w", pid, err)
		}
	}
	return false, nil
}

func procPidHasOpenFiles(base, dir string) (bool, error) {
	for _, link := range []string{"cwd", "root", "exe"} {
		target, err := os.Readlink(filepath.Join(base, link))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if underDir(target, dir) {
			return true, nil
		}
	}

	fds, err := os.ReadDir(filepath.Join(base, "fd"))
	if err != nil {
		return false, err
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if underDir(strings.TrimSuffix(target, " (deleted)"), dir) {
			return true, nil
		}
	}

	maps, err := os.ReadFile(filepath.Join(base, "maps"))
	if err != nil {
		return false, err
	}
	for line := range strings.SplitSeq(string(maps), "\n") {
		fields := strings.SplitN(line, " ", 6)
		if len(fields) < 6 {
			continue
		}
		if underDir(strings.TrimSuffix(strings.TrimSpace(fields[5]), " (deleted)"), dir) {
			return true, nil
		}
	}
	return false, nil
}
