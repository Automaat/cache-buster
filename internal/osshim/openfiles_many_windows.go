//go:build windows

package osshim

import "context"

// OpenFilesUnder always fails closed on Windows, like HasOpenFiles.
func OpenFilesUnder(context.Context, []string) (map[string]bool, error) {
	return nil, ErrOpenFilesUnsupported
}
