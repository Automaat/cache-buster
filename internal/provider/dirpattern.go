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

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/osshim"
	"github.com/Automaat/cache-buster/pkg/size"
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
	for _, path := range cfg.Paths {
		if !strings.ContainsAny(path, "*?[") {
			return nil, fmt.Errorf("path %q must contain a glob (*, ? or [)", path)
		}
		if !config.IsAbsPortable(path) && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
			return nil, fmt.Errorf("path %q must be absolute or start with ~/", path)
		}
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
	size    int64
	files   int64
}

// Clean implements Provider. Smart and full modes behave the same: only
// whole stale directories are removed, never individual files.
func (p *DirPatternProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	var (
		out    strings.Builder
		result CleanResult
		errs   []error
	)

	for _, dir := range p.paths {
		if err := ctx.Err(); err != nil {
			result.Output = out.String()
			return result, err
		}

		sc, reason := p.evaluate(ctx, dir)
		if reason != "" {
			fmt.Fprintf(&out, "skip: %s (%s)\n", dir, reason)
			continue
		}

		idle := p.now().Sub(sc.newest).Round(time.Second)
		if opts.DryRun {
			fmt.Fprintf(&out, "would remove: %s (%s, idle %s)\n", dir, size.FormatSize(sc.size), idle)
			result.BytesCleaned += sc.size
			result.FilesDeleted += sc.files
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
	}

	result.Output = out.String()
	return result, errors.Join(errs...)
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
			sc.size += info.Size()
			sc.files++
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
