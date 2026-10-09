//go:build windows

package osshim

import "context"

// OpenFilesUnder always returns ErrOpenFilesUnsupported on Windows, like
// HasOpenFiles.
func OpenFilesUnder(context.Context, []string) (map[string]bool, error) {
	return nil, ErrOpenFilesUnsupported
}
