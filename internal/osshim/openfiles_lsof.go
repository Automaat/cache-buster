//go:build !linux && !windows

package osshim

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// HasOpenFiles reports whether any process has a file open under dir, using
// lsof. An error means the answer is unknown and must count as open.
// lsof exits 1 with no output when nothing is open.
func HasOpenFiles(ctx context.Context, dir string) (bool, error) {
	path, err := exec.LookPath("lsof")
	if err != nil {
		return false, errors.New("lsof not found")
	}

	out, err := exec.CommandContext(ctx, path, "-t", "+D", dir).Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		return true, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return false, fmt.Errorf("lsof could not inspect every file: %s", msg)
			}
			return false, nil
		}
		return false, err
	}
	return false, nil
}
