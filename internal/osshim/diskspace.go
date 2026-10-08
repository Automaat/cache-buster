package osshim

// DiskSpace reports the space of the filesystem holding a path. Free counts
// only what an unprivileged user can use.
type DiskSpace struct {
	Free  uint64
	Total uint64
}
