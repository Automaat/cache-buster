//go:build linux

package osshim

import "context"

// ProcessCommandLines returns the command line of every running process,
// read from /proc.
func ProcessCommandLines(ctx context.Context) ([]string, error) {
	return commandLinesFromProc(ctx, "/proc")
}
