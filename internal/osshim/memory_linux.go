//go:build linux

package osshim

import (
	"context"
	"os"
)

const procMeminfo = "/proc/meminfo"

func readMemory(_ context.Context) (Memory, error) {
	return readMeminfoFile(procMeminfo)
}

func readMeminfoFile(path string) (Memory, error) {
	f, err := os.Open(path)
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()
	return ParseMeminfo(f)
}
