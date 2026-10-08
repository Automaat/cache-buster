package provider

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/osshim"
	"github.com/smykla-skalski/bilgie/pkg/size"
)

// defaultMinIdle is how long a directory must be untouched before removal.
const defaultMinIdle = 2 * time.Hour

// DirPatternProvider removes whole stale directories matching a glob.
// A directory is only removed when it is idle, has no open files and is not
// a git worktree; every skip is reported with its reason.
type DirPatternProvider struct {
	*BaseProvider
	minIdle           time.Duration
	skipIfOpen        bool
	skipIfGitWorktree bool

	now       func() time.Time
	openCheck func(ctx context.Context, dir string) (bool, error)
	protected []string
	// keep holds directories a match must neither equal nor contain, so a
	// sweep never removes the temp dir the process itself runs in.
	keep []string
}

// NewDirPatternProvider creates a provider that removes stale directories matching cfg.Paths.
func NewDirPatternProvider(name string, cfg config.Provider) (*DirPatternProvider, error) {
	if err := cfg.DirPatternError(); err != nil {
		return nil, err
	}

	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	minIdle := defaultMinIdle
	if cfg.MinIdle != "" {
		minIdle, err = config.ParseDuration(cfg.MinIdle)
		if err != nil {
			return nil, fmt.Errorf("parse min_idle: %w", err)
		}
	}

	p := &DirPatternProvider{
		BaseProvider:      base,
		minIdle:           minIdle,
		skipIfOpen:        cfg.SkipIfOpen == nil || *cfg.SkipIfOpen,
		skipIfGitWorktree: cfg.SkipIfGitWorktree == nil || *cfg.SkipIfGitWorktree,
		now:               time.Now,
		openCheck:         osshim.HasOpenFiles,
	}

	p.keep = []string{os.TempDir()}
	if resolved, evalErr := filepath.EvalSymlinks(os.TempDir()); evalErr == nil {
		p.keep = append(p.keep, resolved)
	}

	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		p.protected = append(p.protected, home)
		if resolved, evalErr := filepath.EvalSymlinks(home); evalErr == nil && resolved != home {
			p.protected = append(p.protected, resolved)
		}
	}

	return p, nil
}

// dirScan is the result of walking one candidate directory.
type dirScan struct {
	newest  time.Time
	gitPath string
	// links holds the hard-linked files of the walk, kept out of size: an
	// inode frees space only when every link to it is removed.
	links map[osshim.FileID]linkInfo
	size  int64
	files int64
}

// linkInfo is one hard-linked inode: its size, its total link count on disk
// and how many of those links the walk found.
type linkInfo struct {
	size        int64
	nlink, seen uint64
}

// evaluated is one matched directory after the eligibility checks.
type evaluated struct {
	dir    string
	reason string
	sc     dirScan
}

// Clean implements Provider. Smart and full modes behave the same: only
// whole stale directories are removed, never individual files.
//
// Every directory is evaluated before any is removed: removing one drops the
// link count of files it shares with another, which would hide the shared
// inode from the later scan.
func (p *DirPatternProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	var (
		out    strings.Builder
		result CleanResult
		errs   []error
		evals  = make([]evaluated, 0, len(p.paths))
		seen   = make(map[string]bool, len(p.paths))
	)

	for _, dir := range p.paths {
		if err := ctx.Err(); err != nil {
			result.Output = out.String()
			return result, err
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		sc, reason := p.evaluate(ctx, dir)
		evals = append(evals, evaluated{dir: dir, sc: sc, reason: reason})
	}
	freed := newFreedLinks(evals)
	eligible := 0
	for _, ev := range evals {
		if ev.reason == "" {
			eligible++
		}
	}

	for _, ev := range evals {
		if err := ctx.Err(); err != nil {
			result.Output = out.String()
			return result, err
		}

		dir, sc, reason := ev.dir, ev.sc, ev.reason
		if reason == "" && !opts.DryRun && eligible > 1 {
			// Other directories were evaluated since this one; confirm it
			// is still idle and clean of .git before deleting.
			sc, reason = p.recheck(ctx, dir, sc)
		}
		if reason != "" {
			fmt.Fprintf(&out, "skip: %s (%s)\n", dir, reason)
			result.SkippedEntries++
			continue
		}

		sc.size = ev.sc.size + freed.bytesFor(ev.sc)
		idle := p.now().Sub(sc.newest).Round(time.Second)
		if opts.DryRun {
			fmt.Fprintf(&out, "would remove: %s (%s, idle %s)\n", dir, size.FormatSize(sc.size), idle)
			result.BytesCleaned += sc.size
			result.FilesDeleted += sc.files
			result.Entries = append(result.Entries, Entry{Path: dir, Size: sc.size})
			continue
		}

		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", dir, err))
			fmt.Fprintf(&out, "error: %s (%v)\n", dir, err)
			continue
		}

		fmt.Fprintf(&out, "removed: %s (%s, idle %s)\n", dir, size.FormatSize(sc.size), idle)
		result.BytesCleaned += sc.size
		result.FilesDeleted += sc.files
		result.Entries = append(result.Entries, Entry{Path: dir, Size: sc.size})
	}

	result.Output = out.String()
	return result, errors.Join(errs...)
}

// freedLinks tracks hard-linked inodes across the directories about to be
// removed. An inode counts as freed once, in the first directory holding it,
// and only when every link to it lies inside those directories.
type freedLinks struct {
	total   map[osshim.FileID]uint64
	claimed map[osshim.FileID]bool
}

func newFreedLinks(evals []evaluated) *freedLinks {
	f := &freedLinks{total: make(map[osshim.FileID]uint64), claimed: make(map[osshim.FileID]bool)}
	for _, ev := range evals {
		if ev.reason != "" {
			continue
		}
		for id, li := range ev.sc.links {
			f.total[id] += li.seen
		}
	}
	return f
}

func (f *freedLinks) bytesFor(sc dirScan) int64 {
	var n int64
	for id, li := range sc.links {
		if f.claimed[id] || f.total[id] < li.nlink {
			continue
		}
		f.claimed[id] = true
		n += li.size
	}
	return n
}

// evaluate decides whether dir may be removed. A non-empty reason means skip.
// Cheap checks run first; the expensive open-files check runs last.
func (p *DirPatternProvider) evaluate(ctx context.Context, dir string) (sc dirScan, skipReason string) {
	if p.isProtected(dir) || p.isProtected(resolveParent(dir)) {
		return sc, "protected path"
	}

	if p.containsKept(dir) || p.containsKept(resolveParent(dir)) {
		return sc, "contains the temp dir in use"
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return sc, "cannot stat: " + err.Error()
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return sc, "symlink"
	}
	if !info.IsDir() {
		return sc, "not a directory"
	}

	if p.skipIfGitWorktree && filepath.Base(dir) == ".git" {
		return sc, "is a .git directory"
	}

	sc, skipReason = scanDir(ctx, dir, info.ModTime())
	if skipReason != "" {
		return sc, skipReason
	}

	if p.skipIfGitWorktree && sc.gitPath != "" {
		return sc, "contains .git at " + sc.gitPath
	}

	if idle := p.now().Sub(sc.newest); idle < p.minIdle {
		return sc, fmt.Sprintf("modified %s ago, min_idle %s", idle.Round(time.Second), p.minIdle)
	}

	if p.skipIfOpen {
		open, openErr := p.openCheck(ctx, dir)
		if openErr != nil {
			return sc, "open-file check failed: " + openErr.Error()
		}
		if open {
			return sc, "has open files"
		}
	}

	return p.recheck(ctx, dir, sc)
}

// recheck rescans after the slow open-files check so activity that started
// meanwhile (new writes, a fresh .git) still blocks removal.
func (p *DirPatternProvider) recheck(ctx context.Context, dir string, before dirScan) (sc dirScan, skipReason string) {
	sc, skipReason = scanDir(ctx, dir, before.newest)
	if skipReason != "" {
		return sc, skipReason
	}
	if sc.newest.After(before.newest) {
		return sc, "modified during checks"
	}
	if p.skipIfGitWorktree && sc.gitPath != "" {
		return sc, "contains .git at " + sc.gitPath
	}
	return sc, ""
}

// resolveParent returns dir with symlinks in its parent chain resolved, so a
// pattern reaching home or root through a symlink is still recognised. The
// final component stays unresolved: a symlink match is skipped separately.
func resolveParent(dir string) string {
	parent, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		return dir
	}
	return filepath.Join(parent, filepath.Base(dir))
}

// isProtected guards against patterns that resolve to the filesystem root,
// the home directory, one of its ancestors, or a direct child of root or home.
func (p *DirPatternProvider) isProtected(dir string) bool {
	clean := filepath.Clean(dir)
	parent := filepath.Dir(clean)
	if !filepath.IsAbs(clean) || parent == clean || filepath.Dir(parent) == parent {
		return true
	}
	for _, prot := range p.protected {
		prot = filepath.Clean(prot)
		if clean == prot || filepath.Dir(clean) == prot || strings.HasPrefix(prot, clean+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// containsKept reports whether dir is, or is an ancestor of, a kept directory.
func (p *DirPatternProvider) containsKept(dir string) bool {
	clean := filepath.Clean(dir)
	for _, k := range p.keep {
		k = filepath.Clean(k)
		if clean == k || strings.HasPrefix(k, clean+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// scanDir walks dir without following symlinks, collecting the newest mtime,
// the first .git entry and size totals. Any unreadable entry yields a skip
// reason: an incomplete scan cannot prove the directory is idle.
func scanDir(ctx context.Context, dir string, rootMod time.Time) (sc dirScan, skipReason string) {
	sc = dirScan{newest: rootMod}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}

		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		var (
			id     osshim.FileID
			nlink  uint64
			shared bool
		)
		if info.Mode().IsRegular() {
			id, nlink, shared = osshim.SharedFileID(path, info)
			if shared {
				// Directory entries of hard-linked files can carry stale
				// times on Windows; the open above refreshes them.
				if fresh, statErr := os.Lstat(path); statErr == nil {
					info = fresh
				}
			}
		}
		if info.ModTime().After(sc.newest) {
			sc.newest = info.ModTime()
		}

		if d.Name() == ".git" && sc.gitPath == "" && path != dir {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = path
			}
			sc.gitPath = rel
		}

		if info.Mode().IsRegular() {
			sc.files++
			if shared {
				if sc.links == nil {
					sc.links = make(map[osshim.FileID]linkInfo)
				}
				li := sc.links[id]
				li.size, li.nlink = info.Size(), nlink
				li.seen++
				sc.links[id] = li
				return nil
			}
			sc.size += info.Size()
		}
		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return sc, "scan interrupted: " + ctxErr.Error()
		}
		return sc, "scan failed: " + err.Error()
	}

	return sc, ""
}
