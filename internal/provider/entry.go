package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

// EntryProvider cleans caches whose top-level entries are only usable whole
// (model snapshots, browser installs, VM images). It removes whole entries,
// oldest first, never individual files inside one.
type EntryProvider struct {
	*BaseProvider
}

// NewEntryProvider creates a provider that deletes whole top-level entries.
func NewEntryProvider(name string, cfg config.Provider) (*EntryProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	return &EntryProvider{BaseProvider: base}, nil
}

type cacheEntry struct {
	modTime time.Time
	path    string
	size    int64
}

// Clean implements Provider. Both modes only trim to max_size: file mtimes
// record download time, not use, so max_age would evict models and
// browsers still in daily use.
func (p *EntryProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	entries, total, err := p.listEntries(ctx)
	if err != nil {
		return CleanResult{}, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].modTime.Before(entries[j].modTime) })

	var (
		freed   int64
		removed int64
		output  strings.Builder
	)
	for _, e := range entries {
		if total-freed <= p.maxSize {
			continue
		}
		if err := ctx.Err(); err != nil {
			return CleanResult{BytesCleaned: freed, FilesDeleted: removed, Output: "interrupted"}, err
		}
		if opts.DryRun {
			fmt.Fprintf(&output, "would remove: %s (%s)\n", e.path, size.FormatSize(e.size))
		} else if err := os.RemoveAll(e.path); err != nil {
			fmt.Fprintf(&output, "error removing %s: %v\n", e.path, err)
			continue
		}
		freed += e.size
		removed++
	}

	res := CleanResult{BytesCleaned: freed, FilesDeleted: removed}
	switch {
	case opts.DryRun:
		res.Output = strings.TrimSpace(output.String())
	case output.Len() > 0:
		res.Output = strings.TrimSpace(output.String())
	case removed == 0:
		res.Output = "already under limit"
	default:
		res.Output = fmt.Sprintf("removed %d entries", removed)
	}
	return res, nil
}

func (p *EntryProvider) listEntries(ctx context.Context) (entries []cacheEntry, total int64, err error) {
	for _, base := range p.paths {
		dirents, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, 0, err
		}
		for _, d := range dirents {
			path := filepath.Join(base, d.Name())
			info, err := os.Lstat(path)
			if err != nil {
				continue
			}
			listing, err := cache.ListFilesContext(ctx, []string{path})
			if err != nil {
				return nil, 0, err
			}
			// Newest file wins: a directory mtime misses writes below it.
			e := cacheEntry{path: path, modTime: info.ModTime()}
			for _, f := range listing.Files {
				e.size += f.Size
				if f.ModTime.After(e.modTime) {
					e.modTime = f.ModTime
				}
			}
			entries = append(entries, e)
			total += e.size
		}
	}
	return entries, total, nil
}
