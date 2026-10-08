//go:build linux

package osshim

import "context"

// ProcessTable returns every running process, read from /proc.
func ProcessTable(ctx context.Context) ([]Process, error) {
	return processTableFromProc(ctx, "/proc")
}
