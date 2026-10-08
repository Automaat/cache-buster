//go:build !linux && !openbsd && !dragonfly && !darwin && !freebsd && !netbsd && !windows

package provider

import (
	"io/fs"
	"time"
)

// fileAtime falls back to the mtime where no access time is available.
func fileAtime(info fs.FileInfo) time.Time {
	return info.ModTime()
}
