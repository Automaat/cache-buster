package provider

import "github.com/smykla-skalski/bilgie/internal/cache"

// Entry is one path a clean removed, or a dry-run would remove, with its size.
type Entry struct {
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

func entriesFromFiles(files []cache.FileInfo) []Entry {
	if len(files) == 0 {
		return nil
	}
	entries := make([]Entry, len(files))
	for i, f := range files {
		entries[i] = Entry{Path: f.Path, Size: f.Size}
	}
	return entries
}
