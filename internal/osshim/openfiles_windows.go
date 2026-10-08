//go:build windows

package osshim

import "context"

// HasOpenFiles always fails closed: Windows offers no supported way to list
// which processes hold handles under a directory, so callers must treat the
// error as "open" and skip the directory.
func HasOpenFiles(context.Context, string) (bool, error) {
	return false, ErrOpenFilesUnsupported
}
