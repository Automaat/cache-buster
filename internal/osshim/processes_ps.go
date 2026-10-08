//go:build !linux && !windows

package osshim

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// ProcessCommandLines returns the command line of every running process,
// listed with ps.
func ProcessCommandLines(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "command=").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(bytes.TrimSpace(out)), "\n"), nil
}
