//go:build linux

package osshim

import "golang.org/x/sys/unix"

// blockUnit is the unit statfs counts blocks in: Linux reports it in Frsize,
// which differs from Bsize on some filesystems.
func blockUnit(st *unix.Statfs_t) uint64 {
	if frsize := toUint64(st.Frsize); frsize > 0 {
		return frsize
	}
	return toUint64(st.Bsize)
}
