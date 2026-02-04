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
	Errors       []error
	FreedBytes   int64
	DeletedCount int64
}

// Trim deletes files that are:
// - older than MaxAge, OR
// - oldest files until total ≤ MaxSize (with 10% buffer).
func Trim(ctx context.Context, paths []string, opts TrimOptions) (TrimResult, error) {
	files, err := ListFiles(paths)
	if err != nil {
		return TrimResult{}, err
	}

	if len(files) == 0 {
		return TrimResult{Output: "no files found"}, nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	var (
		totalSize     int64
		result        TrimResult
		deleteErrors  []string
		output        strings.Builder
		cutoff        = time.Now().Add(-opts.MaxAge)
		targetSize    = int64(float64(opts.MaxSize) * 0.9) // 10% buffer
		toDelete      []FileInfo
		remainingSize int64
	)

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
			deleteErrors = append(deleteErrors, fmt.Sprintf("%s: %v", f.Path, err))
			result.Errors = append(result.Errors, err)
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
	if len(deleteErrors) > 0 {
		result.Output += fmt.Sprintf(" (%d errors: %s)", len(deleteErrors), strings.Join(deleteErrors, "; "))
	}

	return result, nil
}
