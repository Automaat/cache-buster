package cache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// ExpandPaths expands ~ and globs in path patterns.
func ExpandPaths(patterns []string) ([]string, error) {
	var result []string

	for _, pattern := range patterns {
		expanded, err := expandTilde(pattern)
		if err != nil {
			return nil, err
		}

		if strings.ContainsAny(expanded, "*?[") {
			matches, err := filepath.Glob(expanded)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", pattern, err)
			}
			result = append(result, matches...)
		} else {
			result = append(result, expanded)
		}
	}

	return result, nil
}

// CalculateSize calculates total size of all files under given paths.
// Walks directories in parallel.
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
// Walks directories in parallel.
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

func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}

	return path, nil
}
