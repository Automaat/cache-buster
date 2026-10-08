//go:build unix

package osshim

import (
	"io/fs"
	"syscall"
)

type statInt interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// idBits reinterprets a stat field; its width and signedness differ per OS.
func idBits[T statInt](v T) uint64 {
	return uint64(v)
}

// SharedFileID returns the identity of a file that has more than one hard
// link, using device and inode. ok is false for single-link files, so callers
// count those directly.
func SharedFileID(_ string, info fs.FileInfo) (id FileID, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat || idBits(st.Nlink) < 2 {
		return FileID{}, false
	}
	return FileID{Volume: idBits(st.Dev), Index: idBits(st.Ino)}, true
}
