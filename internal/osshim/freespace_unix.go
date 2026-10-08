//go:build unix

package osshim

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// QueryDiskSpace returns the free and total bytes of the filesystem holding
// path.
func QueryDiskSpace(path string) (DiskSpace, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return DiskSpace{}, err
	}
	free, err := blocksToBytes(st.Bavail, st.Bsize)
	if err != nil {
		return DiskSpace{}, err
	}
	total, err := blocksToBytes(st.Blocks, st.Bsize)
	if err != nil {
		return DiskSpace{}, err
	}
	return DiskSpace{Free: free, Total: total}, nil
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
