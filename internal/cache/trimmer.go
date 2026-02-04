package cache

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Automaat/cache-buster/pkg/size"
)

// TrimOptions configures cache trimming.
type TrimOptions struct {
	MaxSize int64         // Target size (10% buffer applied internally)
	MaxAge  time.Duration // Delete files older than this
	DryRun  bool
}

// TrimResult contains trimming operation results.
type TrimResult struct {
	Output       string
	Errors       []AccessError
	DeletedCount int64
	FreedBytes   int64
}

const trimBufferFactor = 0.9 // Keep 10% headroom below max_size

// Trim deletes files that are:
// - older than MaxAge, OR
// - oldest files until total ≤ MaxSize (with 10% buffer).
func Trim(ctx context.Context, paths []string, opts TrimOptions) (TrimResult, error) {
	listResult, err := ListFiles(paths)
	if err != nil {
		return TrimResult{}, err
	}

	files := listResult.Files
	if len(files) == 0 {
		return TrimResult{Output: "no files found", Errors: listResult.Warnings}, nil
	}

	// Sort by ModTime (oldest first). ModTime is appropriate for dev caches
	// where content-addressable storage updates mtime on access.
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	var (
		totalSize     int64
		result        TrimResult
		deleteErrors  []AccessError
		output        strings.Builder
		cutoff        = time.Now().Add(-opts.MaxAge)
		targetSize    = int64(float64(opts.MaxSize) * trimBufferFactor)
		toDelete      []FileInfo
		remainingSize int64
	)

	// Carry forward scan warnings
	deleteErrors = append(deleteErrors, listResult.Warnings...)

	for _, f := range files {
		totalSize += f.Size
	}

	// Phase 1: mark files older than MaxAge for deletion
	for _, f := range files {
		if f.ModTime.Before(cutoff) {
			toDelete = append(toDelete, f)
		} else {
			remainingSize += f.Size
		}
	}

	// Phase 2: if still over target, delete oldest remaining files
	if remainingSize > targetSize {
		var remaining []FileInfo
		for _, f := range files {
			if f.ModTime.Before(cutoff) {
				continue // already marked
			}
			remaining = append(remaining, f)
		}

		for _, f := range remaining {
			if remainingSize <= targetSize {
				break
			}
			toDelete = append(toDelete, f)
			remainingSize -= f.Size
		}
	}

	// Execute deletions
	for _, f := range toDelete {
		select {
		case <-ctx.Done():
			result.Output = "interrupted"
			return result, ctx.Err()
		default:
		}

		if opts.DryRun {
			age := time.Since(f.ModTime).Truncate(time.Hour)
			fmt.Fprintf(&output, "would delete: %s (%s, age: %s)\n", f.Path, size.FormatSize(f.Size), age)
			result.FreedBytes += f.Size
			result.DeletedCount++
			continue
		}

		if err := os.Remove(f.Path); err != nil {
			deleteErrors = append(deleteErrors, ClassifyError(f.Path, err))
			continue
		}

		result.FreedBytes += f.Size
		result.DeletedCount++
	}

	if opts.DryRun {
		result.Output = output.String()
		return result, nil
	}

	result.Output = fmt.Sprintf("deleted %d files", result.DeletedCount)
	result.Errors = deleteErrors

	return result, nil
}
