package auto

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
)

// Unmanaged scan defaults.
const (
	DefaultUnmanagedTop    = 10
	DefaultUnmanagedBudget = 10 * time.Second
	DefaultUnmanagedMin    = int64(100) << 20
	unmanagedWorkers       = 4
)

// UnmanagedDir is a top-level directory no provider covers. Partial means the
// time budget ran out while measuring it, so Bytes is a lower bound.
type UnmanagedDir struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Partial bool   `json:"partial,omitempty"`
}

// UnmanagedReport is the result of ScanUnmanaged. Incomplete means the budget
// or the context ended the scan before every directory was fully measured.
type UnmanagedReport struct {
	Dirs       []UnmanagedDir `json:"dirs"`
	Incomplete bool           `json:"incomplete,omitempty"`
}

// ScanOptions bounds one unmanaged scan.
type ScanOptions struct {
	Roots    []string
	Covered  []string
	Top      int
	MinBytes int64
	Budget   time.Duration
}

// UnmanagedRoots lists the locations where untracked caches pile up. It is the
// platform seam: other operating systems add their own roots here.
func UnmanagedRoots(home string) []string {
	roots := []string{os.TempDir()}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".local", "share"))
		switch runtime.GOOS {
		case "darwin":
			roots = append(roots, filepath.Join(home, "Library", "Caches"))
		case "windows":
			roots = append(roots, localAppData(home))
		default:
			roots = append(roots, envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")))
			roots[1] = envOr("XDG_DATA_HOME", roots[1])
		}
	}
	if runtime.GOOS == "darwin" {
		roots = append(roots, "/private/tmp")
	}
	return roots
}

// CoveredPaths returns the concrete paths providers manage: every enabled
// provider plus the directory-pattern sweeps that auto runs even when
// disabled. Globs are expanded to what exists now.
func CoveredPaths(cfg *config.Config) []string {
	var out []string
	for name := range cfg.Providers {
		pc := cfg.Providers[name]
		if !cfg.Applies(name) || (!pc.Enabled && pc.Type != config.TypeDirPattern) {
			continue
		}
		paths, err := config.ExpandPaths(pc.Paths)
		if err != nil {
			paths = literalPrefixes(pc.Paths)
		}
		out = append(out, paths...)
	}
	return out
}

// literalPrefixes is the fallback when a pattern cannot be expanded: the
// directory part before the first glob character still marks what the
// provider covers, so its directories are not reported as unmanaged.
func literalPrefixes(patterns []string) []string {
	var out []string
	for _, pattern := range patterns {
		expanded, err := config.ExpandTilde(pattern)
		if err != nil {
			continue
		}
		if i := strings.IndexAny(expanded, "*?["); i >= 0 {
			expanded = filepath.Dir(expanded[:i] + "x")
		}
		out = append(out, expanded)
	}
	return out
}

// ScanUnmanaged measures the top-level directories under opts.Roots that no
// covered path overlaps, and returns the largest. Only top-level directory
// totals are computed, workers are bounded, and the whole scan stops at
// opts.Budget or when ctx is cancelled.
func ScanUnmanaged(ctx context.Context, opts ScanOptions) UnmanagedReport {
	if opts.Budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Budget)
		defer cancel()
	}

	covered := resolvedVariants(opts.Covered)
	var candidates []string
	seen := make(map[string]bool)
	for _, root := range dedupe(opts.Roots) {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return UnmanagedReport{Incomplete: true}
			}
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			resolved := resolve(path)
			if seen[resolved] || overlapsAny(resolved, covered) {
				continue
			}
			seen[resolved] = true
			candidates = append(candidates, path)
		}
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		dirs   []UnmanagedDir
		report UnmanagedReport
		work   = make(chan string)
	)
	for range unmanagedWorkers {
		wg.Go(func() {
			for path := range work {
				bytes, complete := measureDir(ctx, path)
				mu.Lock()
				dirs = append(dirs, UnmanagedDir{Path: path, Bytes: bytes, Partial: !complete})
				if !complete {
					report.Incomplete = true
				}
				mu.Unlock()
			}
		})
	}
feed:
	for _, path := range candidates {
		select {
		case work <- path:
		case <-ctx.Done():
			mu.Lock()
			report.Incomplete = true
			mu.Unlock()
			break feed
		}
	}
	close(work)
	wg.Wait()

	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].Bytes != dirs[j].Bytes {
			return dirs[i].Bytes > dirs[j].Bytes
		}
		return dirs[i].Path < dirs[j].Path
	})
	for _, d := range dirs {
		if d.Bytes < opts.MinBytes {
			break
		}
		report.Dirs = append(report.Dirs, d)
	}
	if opts.Top > 0 && len(report.Dirs) > opts.Top {
		report.Dirs = report.Dirs[:opts.Top]
	}
	return report
}

func measureDir(ctx context.Context, root string) (int64, bool) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += diskUsage(info)
		}
		return nil
	})
	return total, err == nil
}

func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

func resolvedVariants(paths []string) []string {
	out := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		out = append(out, filepath.Clean(p))
		if r := resolve(p); r != filepath.Clean(p) {
			out = append(out, r)
		}
	}
	return out
}

func dedupe(paths []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range paths {
		r := resolve(p)
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

func overlapsAny(dir string, covered []string) bool {
	for _, c := range covered {
		if within(dir, c) || within(c, dir) {
			return true
		}
	}
	return false
}

func localAppData(home string) string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir
	}
	return filepath.Join(home, "AppData", "Local")
}

func envOr(name, fallback string) string {
	if dir := os.Getenv(name); filepath.IsAbs(dir) {
		return dir
	}
	return fallback
}
