//go:build windows

package osshim

import "context"

// ProcessCwds always fails closed: Windows offers no supported way to read
// another process's working directory.
func ProcessCwds(context.Context, []int) (map[int]string, error) {
	return nil, ErrCwdUnsupported
}
