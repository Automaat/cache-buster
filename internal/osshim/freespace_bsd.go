//go:build unix && !linux

package osshim

import "golang.org/x/sys/unix"

func blockUnit(st *unix.Statfs_t) uint64 {
	return toUint64(st.Bsize)
}
