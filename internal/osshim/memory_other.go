//go:build !linux && !windows

package osshim

import (
	"context"
	"os/exec"
)

func readMemory(ctx context.Context) (Memory, error) {
	return readMemoryWith(ctx, runOutput)
}

func runOutput(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
