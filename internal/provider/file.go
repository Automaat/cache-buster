package provider

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

// FileProvider cleans caches by deleting oldest files until under limit.
type FileProvider struct {
	*BaseProvider
}

// NewFileProvider creates a provider that cleans by file deletion.
func NewFileProvider(name string, cfg config.Provider) (*FileProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	return &FileProvider{
		BaseProvider: base,
	}, nil
}

func (p *FileProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	currentSize, err := p.CurrentSize()
	if err != nil {
		return CleanResult{}, err
	}

	if currentSize <= p.maxSize {
		return CleanResult{
			Output: "already under limit",
		}, nil
	}

	files, err := cache.ListFiles(p.paths)
	if err != nil {
		return CleanResult{}, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	var (
		bytesToDelete = currentSize - p.maxSize
		bytesDeleted  int64
		filesDeleted  int64
		output        string
	)

	for _, f := range files {
		if bytesDeleted >= bytesToDelete {
			break
		}

		select {
		case <-ctx.Done():
			return CleanResult{
				BytesCleaned: bytesDeleted,
				FilesDeleted: filesDeleted,
				Output:       "interrupted",
			}, ctx.Err()
		default:
		}

		if opts.DryRun {
			output += fmt.Sprintf("would delete: %s (%s)\n", f.Path, size.FormatSize(f.Size))
			bytesDeleted += f.Size
			filesDeleted++
			continue
		}

		if err := os.Remove(f.Path); err != nil {
			continue
		}

		bytesDeleted += f.Size
		filesDeleted++
	}

	if opts.DryRun {
		return CleanResult{
			BytesCleaned: bytesDeleted,
			FilesDeleted: filesDeleted,
			Output:       output,
		}, nil
	}

	return CleanResult{
		BytesCleaned: bytesDeleted,
		FilesDeleted: filesDeleted,
		Output:       fmt.Sprintf("deleted %d files", filesDeleted),
	}, nil
}
