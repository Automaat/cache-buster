//go:build unix

package osshim

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// FreeSpace returns the bytes available to an unprivileged user on the
// filesystem holding path.
func FreeSpace(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return blocksToBytes(st.Bavail, st.Bsize)
}

type integer interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~int | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uint
}

// blocksToBytes multiplies a block count by a block size whose integer types
// differ per OS and architecture, rejecting values that make no sense.
func blocksToBytes[B, S integer](blocks B, size S) (uint64, error) {
	if size <= 0 || blocks < 0 {
		return 0, fmt.Errorf("invalid filesystem geometry: %d blocks of %d bytes", blocks, size)
	}
	return uint64(blocks) * uint64(size), nil
}
