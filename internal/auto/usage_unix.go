//go:build unix

package auto

import (
	"io/fs"
	"syscall"
)

// diskUsage is the space a file occupies, so sparse files such as VM images
// do not report their apparent size.
func diskUsage(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}
