//go:build windows

package osshim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

const (
	pageFileBufStart  = 4 << 10
	pageFileBufMax    = 1 << 20
	statusLenMismatch = windows.NTStatus(0xC0000004)
)

func readMemory(_ context.Context) (Memory, error) {
	st := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return Memory{}, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	used, total, err := queryPageFiles()
	if err != nil {
		return Memory{}, err
	}
	return Memory{
		MemTotal: st.TotalPhys, MemAvailable: st.AvailPhys,
		SwapUsed: used, SwapTotal: total, FreePercent: -1,
	}, nil
}

// queryPageFiles reads real page file usage, not commit charge: committed
// memory that was never paged out is not swap.
func queryPageFiles() (used, total uint64, err error) {
	for size := pageFileBufStart; size <= pageFileBufMax; size *= 2 {
		buf := make([]byte, size)
		var ret uint32
		status := windows.NtQuerySystemInformation(windows.SystemPageFileInformation, unsafe.Pointer(&buf[0]), uint32(size), &ret)
		if errors.Is(status, statusLenMismatch) {
			continue
		}
		if status != nil {
			return 0, 0, fmt.Errorf("NtQuerySystemInformation(page files): %w", status)
		}
		if ret == 0 {
			return 0, 0, nil
		}
		return ParsePageFiles(buf[:ret], uint64(os.Getpagesize()))
	}
	return 0, 0, errors.New("page file information does not fit in the buffer")
}
