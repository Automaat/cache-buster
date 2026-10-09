//go:build windows

package osshim

import "context"

// HasOpenFiles always returns ErrOpenFilesUnsupported: Windows offers no
// supported way to list which processes hold handles under a directory, so a
// caller must treat the error as "open" unless it has a check of its own, as
// the rename-aside of the project-artifacts provider is.
func HasOpenFiles(context.Context, string) (bool, error) {
	return false, ErrOpenFilesUnsupported
}
