//go:build linux

package osshim

import (
	"context"
	"os"
	"path/filepath"
)

// HasOpenFiles reports whether any process has a file open under dir,
// inspecting /proc. An error means the answer is unknown and must count as
// open.
func HasOpenFiles(ctx context.Context, dir string) (bool, error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	return procHasOpenFiles(ctx, "/proc", resolved, os.Getuid())
}
