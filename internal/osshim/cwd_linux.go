//go:build linux

package osshim

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
)

// ProcessCwds returns the working directory of each pid it can read. A
// process that exited or belongs to another user is left out of the map.
func ProcessCwds(ctx context.Context, pids []int) (map[int]string, error) {
	return processCwdsFromProc(ctx, "/proc", pids)
}

func processCwdsFromProc(ctx context.Context, root string, pids []int) (map[int]string, error) {
	cwds := make(map[int]string, len(pids))
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cwd, err := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "cwd")); err == nil {
			cwds[pid] = cwd
		}
	}
	return cwds, nil
}
