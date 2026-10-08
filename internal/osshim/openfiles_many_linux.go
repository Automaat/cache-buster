//go:build linux

package osshim

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// procOpenFilesUnder reports, for each of dirs, whether any process holds a
// file inside it, reading every process once. It stops early when all are.
func procOpenFilesUnder(ctx context.Context, root string, dirs []string, selfUID int) (map[string]bool, error) {
	open := make(map[string]bool, len(dirs))
	err := procScan(ctx, root, selfUID, func(paths []string) bool {
		for _, dir := range dirs {
			if !open[dir] && slices.ContainsFunc(paths, func(path string) bool { return underDir(path, dir) }) {
				open[dir] = true
			}
		}
		return len(open) == len(dirs)
	})
	return open, err
}

// OpenFilesUnder reports, for each dir, whether any process has a file open
// inside it, reading /proc once for all of them. A dir that no longer exists
// holds nothing. An error means the answer is unknown and every dir must count
// as open.
func OpenFilesUnder(ctx context.Context, dirs []string) (map[string]bool, error) {
	var live, resolved []string
	for _, dir := range dirs {
		r, err := filepath.EvalSymlinks(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		live, resolved = append(live, dir), append(resolved, r)
	}
	out := make(map[string]bool, len(dirs))
	if len(live) == 0 {
		return out, nil
	}
	byResolved, err := procOpenFilesUnder(ctx, "/proc", resolved, os.Getuid())
	if err != nil {
		return nil, err
	}
	for i, dir := range live {
		out[dir] = byResolved[resolved[i]]
	}
	return out, nil
}
