package auto

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// scanLimits bound the git marker scan of one provider path. Depth counts the
// levels listed below the root, so a marker sits in a directory at most depth
// levels down. Dirs and entries cap the directories listed and names read.
type scanLimits struct {
	timeout time.Duration
	depth   int
	dirs    int
	entries int
}

// markerLimits is the budget of the scan. A checkout buried deeper than
// depth is accepted as unverified by the scan; insideGitCheckout still
// protects every path that lies inside a checkout.
var markerLimits = scanLimits{timeout: 5 * time.Second, depth: 3, dirs: 50000, entries: 2000000}

const listBatch = 2048

type dirReader interface {
	ReadDir(n int) ([]os.DirEntry, error)
	Close() error
}

var openDir = func(path string) (dirReader, error) { return os.Open(path) }

type markerOutcome int

const (
	markerNone markerOutcome = iota
	markerFound
	markerTooLarge
	markerUnreadable
	markerCancelled
)

type markerResult struct {
	detail  string
	outcome markerOutcome
}

// findGitMarker looks for a git checkout or worktree below root: an entry
// named .git, of any kind. A .git directory is a checkout; a .git file is a
// worktree or submodule checkout whose gitdir line points into another
// repository. Both protect alike, so no file is ever opened. The walk lists
// directory names only, with no stat per file, goes breadth first so shallow
// hits come first, never follows symlinks, stops at the first hit, and fails
// closed when the limits, a read error or ctx stop it before the tree is
// verified.
func findGitMarker(ctx context.Context, root string, lim scanLimits) markerResult {
	if lim.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lim.timeout)
		defer cancel()
	}

	level := []string{root}
	dirs, entries := 0, 0
	for depth := 0; len(level) > 0 && depth <= lim.depth; depth++ {
		var next []string
		for _, dir := range level {
			dirs++
			if dirs > lim.dirs {
				return markerResult{outcome: markerTooLarge}
			}
			listing := listDir(ctx, dir, lim.entries-entries, depth < lim.depth)
			if listing.outcome != markerNone {
				return listing.markerResult
			}
			if listing.found {
				return markerResult{outcome: markerFound, detail: dir}
			}
			entries += listing.count
			next = append(next, listing.subdirs...)
		}
		level = next
	}
	return markerResult{}
}

type dirListing struct {
	markerResult
	subdirs []string
	count   int
	found   bool
}

// listDir reads one directory in batches, so a directory with hundreds of
// thousands of files is never held in memory whole, and returns at the first
// .git entry. budget is the number of names still allowed.
func listDir(ctx context.Context, dir string, budget int, wantSubdirs bool) dirListing {
	f, err := openDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return dirListing{}
	}
	if err != nil {
		return dirListing{markerResult: markerResult{outcome: markerUnreadable, detail: "cannot verify: " + dir}}
	}
	defer func() { _ = f.Close() }()

	var out dirListing
	for {
		if ctx.Err() != nil {
			out.outcome = ctxOutcome(ctx)
			return out
		}
		batch, readErr := f.ReadDir(listBatch)
		out.count += len(batch)
		if out.count > budget {
			out.outcome = markerTooLarge
			return out
		}
		for _, e := range batch {
			if e.Name() == ".git" {
				out.found = true
				return out
			}
			if wantSubdirs && e.IsDir() {
				out.subdirs = append(out.subdirs, filepath.Join(dir, e.Name()))
			}
		}
		if errors.Is(readErr, io.EOF) {
			return out
		}
		if readErr != nil {
			return dirListing{markerResult: markerResult{outcome: markerUnreadable, detail: "cannot verify: " + dir}}
		}
	}
}

// ctxOutcome tells the scan's own timeout, a budget overrun, from a caller
// that cancelled.
func ctxOutcome(ctx context.Context) markerOutcome {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return markerTooLarge
	}
	return markerCancelled
}
