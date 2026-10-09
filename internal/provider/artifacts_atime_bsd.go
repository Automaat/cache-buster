//go:build darwin || freebsd || netbsd

package provider

import (
	"io/fs"
	"syscall"
	"time"
)

// fileAtime returns the last access time, or the mtime when the platform does
// not report one.
func fileAtime(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atimespec.Unix())
	}
	return info.ModTime()
}
