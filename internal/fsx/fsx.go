// Package fsx reads files that sit in user-controlled places without ever
// blocking on a FIFO, socket or device someone left at the path.
package fsx

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrNotRegular = errors.New("not a regular file")
	ErrTooLarge   = errors.New("file too large")
)

// CheckRegular is nil for a missing path or a regular file (links followed)
// and an ErrNotRegular error for anything else, so a caller about to open the
// path with a plain os.Open never blocks on a pipe.
func CheckRegular(path string) error {
	if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is a %s", ErrNotRegular, path, kind(info.Mode()))
	}
	return nil
}

// ReadRegular reads at most limit bytes of a regular file. Anything else
// (directory, FIFO, socket, device, a link to one) fails with ErrNotRegular
// without opening it; a file over limit fails with ErrTooLarge. The file is
// opened non-blocking and checked again after the open, so a path swapped for
// a FIFO in between cannot hang the caller.
func ReadRegular(path string, limit int64) ([]byte, error) {
	if err := CheckRegular(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openFlags, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotRegular, path, kind(info.Mode()))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, path, limit)
	}
	return data, nil
}

func kind(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeDevice != 0:
		return "device"
	}
	return "special file"
}
