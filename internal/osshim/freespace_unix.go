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

// blocksToBytes multiplies a block count by a block size whose integer type
// differs per OS, rejecting values that make no sense.
func blocksToBytes[S ~uint32 | ~int64](blocks uint64, size S) (uint64, error) {
	if size <= 0 {
		return 0, fmt.Errorf("invalid filesystem block size %d", size)
	}
	return blocks * uint64(size), nil
}
