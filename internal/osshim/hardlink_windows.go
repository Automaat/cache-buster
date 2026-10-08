//go:build windows

package osshim

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// SharedFileID returns the identity of a file that has more than one hard
// link and its link count, from the volume serial number and file index. It opens path for
// attribute queries only. ok is false for single-link files and when the
// identity cannot be read, so such files are counted in full.
func SharedFileID(path string, _ fs.FileInfo) (id FileID, nlink uint64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return FileID{}, 0, false
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return FileID{}, 0, false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil || fi.NumberOfLinks < 2 {
		return FileID{}, 0, false
	}
	return FileID{
		Volume: uint64(fi.VolumeSerialNumber),
		Index:  uint64(fi.FileIndexHigh)<<32 | uint64(fi.FileIndexLow),
	}, uint64(fi.NumberOfLinks), true
}
