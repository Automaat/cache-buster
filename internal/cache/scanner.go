package cache

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// FileInfo holds file metadata for cache entries.
type FileInfo struct {
	ModTime time.Time
	Path    string
	Size    int64
}

// CalculateSize calculates total size of all files under given paths.
// Walks directories in parallel. Returns 0 if paths is empty.
func CalculateSize(paths []string) (int64, error) {
	var total atomic.Int64
	var firstErr atomic.Value

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // skip inaccessible entries
				}
				if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				total.Add(info.Size())
				return nil
			})
			if err != nil {
				firstErr.CompareAndSwap(nil, err)
			}
		}(path)
	}
	wg.Wait()

	if err, ok := firstErr.Load().(error); ok && err != nil {
		return total.Load(), fmt.Errorf("calculate size: %w", err)
	}
	return total.Load(), nil
}

// ListFiles returns file info for all files under given paths.
// Walks directories in parallel. Returns empty slice if paths is empty.
func ListFiles(paths []string) ([]FileInfo, error) {
	var mu sync.Mutex
	var files []FileInfo
	var firstErr atomic.Value

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // skip inaccessible entries
				}
				if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				fi := FileInfo{
					Path:    path,
					Size:    info.Size(),
					ModTime: info.ModTime(),
				}
				mu.Lock()
				files = append(files, fi)
				mu.Unlock()
				return nil
			})
			if err != nil {
				firstErr.CompareAndSwap(nil, err)
			}
		}(path)
	}
	wg.Wait()

	if err, ok := firstErr.Load().(error); ok && err != nil {
		return files, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}
