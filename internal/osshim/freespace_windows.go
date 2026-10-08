//go:build windows

package osshim

import (
	"golang.org/x/sys/windows"
)

// FreeSpace returns the bytes available to the calling user on the volume
// holding path.
func FreeSpace(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}
	return avail, nil
}
