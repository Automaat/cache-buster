//go:build !unix && !windows

package osshim

import "io/fs"

// SharedFileID reports no identity on platforms without a hard-link probe, so
// every link is counted in full.
func SharedFileID(_ string, _ fs.FileInfo) (id FileID, ok bool) {
	return FileID{}, false
}
