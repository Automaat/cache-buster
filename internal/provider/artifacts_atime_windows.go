//go:build windows

package provider

import (
	"io/fs"
	"syscall"
	"time"
)

// fileAtime returns the last access time, or the mtime when the platform does
// not report one.
func fileAtime(info fs.FileInfo) time.Time {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, data.LastAccessTime.Nanoseconds())
	}
	return info.ModTime()
}
