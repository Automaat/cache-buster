//go:build !unix

package auto

import "io/fs"

func diskUsage(info fs.FileInfo) int64 {
	return info.Size()
}
