//go:build windows

package osshim

import (
	"golang.org/x/sys/windows"
)

// QueryDiskSpace returns the free and total bytes of the volume holding path.
func QueryDiskSpace(path string) (DiskSpace, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DiskSpace{}, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return DiskSpace{}, err
	}
	return DiskSpace{Free: avail, Total: total}, nil
}
