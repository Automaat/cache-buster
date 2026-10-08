//go:build !linux && !windows

package osshim

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// ProcessCwds returns the working directory of each pid it can read, using
// lsof. A process that exited or belongs to another user is left out of the
// map. lsof exits 1 when some pid is gone, so only a missing lsof or an
// empty failure is an error.
func ProcessCwds(ctx context.Context, pids []int) (map[int]string, error) {
	if len(pids) == 0 {
		return map[int]string{}, nil
	}
	path, err := exec.LookPath("lsof")
	if err != nil {
		return nil, errors.New("lsof not found")
	}
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	out, err := exec.CommandContext(ctx, path, "-a", "-d", "cwd", "-Fpn", "-p", strings.Join(list, ",")).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, err
		}
	}
	return parseLsofCwd(string(out)), nil
}
