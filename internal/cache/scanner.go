package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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

// ScanResult contains size calculation results with access warnings.
type ScanResult struct {
	Warnings []AccessError
	Size     int64
}

// CalculateSize calculates total size of all files under given paths.
// Walks directories in parallel. Returns 0 if paths is empty.
// Access errors are collected as warnings rather than stopping the scan.
func CalculateSize(paths []string) (ScanResult, error) {
	var total atomic.Int64
	var firstErr atomic.Value
	var mu sync.Mutex
	var warnings []AccessError

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						accessErr := ClassifyError(path, err)
						mu.Lock()
						warnings = append(warnings, accessErr)
						mu.Unlock()
					}
					return nil
				}
				if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					accessErr := ClassifyError(path, err)
					mu.Lock()
					warnings = append(warnings, accessErr)
					mu.Unlock()
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

	result := ScanResult{
		Size:     total.Load(),
		Warnings: warnings,
	}

	if err, ok := firstErr.Load().(error); ok && err != nil {
		return result, fmt.Errorf("calculate size: %w", err)
	}
	return result, nil
}

// ListResult contains file listing results with access warnings.
type ListResult struct {
	Files    []FileInfo
	Warnings []AccessError
}

// ListFiles returns file info for all files under given paths.
// Walks directories in parallel. Returns empty slice if paths is empty.
// Access errors are collected as warnings rather than stopping the scan.
func ListFiles(paths []string) (ListResult, error) {
	var mu sync.Mutex
	var files []FileInfo
	var warnings []AccessError
	var firstErr atomic.Value

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						accessErr := ClassifyError(path, err)
						mu.Lock()
						warnings = append(warnings, accessErr)
						mu.Unlock()
					}
					return nil
				}
				if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					accessErr := ClassifyError(path, err)
					mu.Lock()
					warnings = append(warnings, accessErr)
					mu.Unlock()
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

	result := ListResult{
		Files:    files,
		Warnings: warnings,
	}

	if err, ok := firstErr.Load().(error); ok && err != nil {
		return result, fmt.Errorf("list files: %w", err)
	}
	return result, nil
}
