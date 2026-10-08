package provider

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type artifactKind string

const (
	kindRust   artifactKind = "rust"
	kindNode   artifactKind = "node"
	kindPython artifactKind = "python"
)

const (
	trashPrefix = ".bilgie-trash-"
	sampleLimit = 3000
	sizeWorkers = 4

	cargoTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"
)

// neverDescend are directory names the project search and the idleness
// sample skip by name, marker or not.
var neverDescend = map[string]bool{
	".git":         true,
	"node_modules": true,
	"target":       true,
	".venv":        true,
	"venv":         true,
}

type artifactDir struct {
	Path  string
	Kind  artifactKind
	Size  int64
	Files int64

	unreadable bool
}

type project struct {
	Dir       string
	root      string
	artifacts []*artifactDir

	measured bool
	problem  string
	repoRoot string
	newest   time.Time
	sampled  time.Time
}

type artifactScan struct {
	projects []*project
	trash    []string
	partial  bool
}

func (s *artifactScan) total() int64 {
	var total int64
	for _, proj := range s.projects {
		for _, art := range proj.artifacts {
			total += art.Size
		}
	}
	return total
}

func trashName(base string) string {
	return trashPrefix + strings.TrimPrefix(base, ".") + "-" + strings.ToLower(cryptorand.Text()[:8])
}

// discover finds every project artifact below the roots and measures it. The
// time budget bounds the search; sizing is bounded by ctx alone.
func (p *ProjectArtifactsProvider) discover(ctx context.Context) (*artifactScan, error) {
	scan := &artifactScan{}
	searchCtx, cancel := context.WithTimeout(ctx, p.budget)
	defer cancel()

	seen := map[string]bool{}
	for _, root := range p.paths {
		resolved, ok := p.usableRoot(root)
		if !ok || seen[resolved] {
			continue
		}
		seen[resolved] = true
		w := &walker{p: p, scan: scan, root: resolved, seen: seen}
		w.walk(searchCtx, resolved, 0)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scan.partial = searchCtx.Err() != nil

	sort.Slice(scan.projects, func(i, j int) bool { return scan.projects[i].Dir < scan.projects[j].Dir })
	sort.Strings(scan.trash)

	if err := sizeArtifacts(ctx, scan); err != nil {
		return nil, err
	}
	return scan, nil
}

// usableRoot resolves a configured root and rejects any that is missing, not
// a directory, the filesystem root, home or a parent of home: scanning those
// would wander into places no project artifact lives.
func (p *ProjectArtifactsProvider) usableRoot(root string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() || filepath.Dir(resolved) == resolved {
		return "", false
	}
	for _, home := range homeSpellings(p.home) {
		if pathWithin(home, resolved) {
			return "", false
		}
	}
	return resolved, true
}

func homeSpellings(home string) []string {
	if home == "" {
		return nil
	}
	out := []string{filepath.Clean(home)}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != out[0] {
		out = append(out, resolved)
	}
	return out
}

type walker struct {
	p    *ProjectArtifactsProvider
	scan *artifactScan
	root string
	seen map[string]bool
}

func (w *walker) walk(ctx context.Context, dir string, depth int) {
	if ctx.Err() != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	byName := make(map[string]fs.DirEntry, len(entries))
	for _, e := range entries {
		byName[e.Name()] = e
	}

	if arts := w.p.detect(dir, byName); len(arts) > 0 {
		w.scan.projects = append(w.scan.projects, &project{Dir: dir, root: w.root, artifacts: arts})
	}

	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(dir, name)
		switch {
		case strings.HasPrefix(name, trashPrefix):
			w.scan.trash = append(w.scan.trash, child)
		case neverDescend[name], depth >= w.p.maxDepth, lexicallyProtected(child), w.seen[child]:
		default:
			w.seen[child] = true
			w.walk(ctx, child, depth+1)
		}
	}
}

// detect returns the artifact directories of one project directory. Every
// kind needs its marker inside the artifact and its project file beside it.
func (p *ProjectArtifactsProvider) detect(dir string, byName map[string]fs.DirEntry) []*artifactDir {
	var out []*artifactDir
	add := func(kind artifactKind, name string) {
		art := &artifactDir{Path: filepath.Join(dir, name), Kind: kind}
		if markersValid(art) {
			out = append(out, art)
		}
	}
	isDir := func(name string) bool {
		e, ok := byName[name]
		return ok && e.IsDir()
	}
	if p.kinds[kindRust] && isDir("target") {
		add(kindRust, "target")
	}
	if p.kinds[kindNode] && isDir("node_modules") {
		add(kindNode, "node_modules")
	}
	if p.kinds[kindPython] {
		for _, name := range []string{".venv", "venv"} {
			if isDir(name) {
				add(kindPython, name)
			}
		}
	}
	return out
}

// markersValid checks the artifact's marker file and its sibling project
// file on disk. A symlinked directory or marker never counts.
func markersValid(art *artifactDir) bool {
	if !isRealDir(art.Path) {
		return false
	}
	project := filepath.Dir(art.Path)
	switch art.Kind {
	case kindRust:
		return isRegular(filepath.Join(project, "Cargo.toml")) && validCargoTag(filepath.Join(art.Path, "CACHEDIR.TAG"))
	case kindNode:
		return isRegular(filepath.Join(project, "package.json"))
	case kindPython:
		return isRegular(filepath.Join(art.Path, "pyvenv.cfg")) && hasPythonProjectFile(project)
	}
	return false
}

func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func isRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func hasPythonProjectFile(project string) bool {
	if isRegular(filepath.Join(project, "pyproject.toml")) {
		return true
	}
	matches, err := filepath.Glob(filepath.Join(escapeGlobMeta(project), "requirements*.txt"))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(matches, isRegular)
}

func escapeGlobMeta(dir string) string {
	return strings.NewReplacer("[", "[[]", "*", "[*]", "?", "[?]").Replace(dir)
}

// validCargoTag accepts only a regular file whose first line is the cache
// directory tag signature cargo writes.
func validCargoTag(path string) bool {
	if !isRegular(path) {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, len(cargoTagSignature)+2)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}
	first, _, _ := bytes.Cut(buf[:n], []byte("\n"))
	return string(bytes.TrimRight(first, "\r")) == cargoTagSignature
}

func sizeArtifacts(ctx context.Context, scan *artifactScan) error {
	work := make(chan *artifactDir)
	var wg sync.WaitGroup
	for range sizeWorkers {
		wg.Go(func() {
			for art := range work {
				art.Size, art.Files, art.unreadable = measureTree(ctx, art.Path)
			}
		})
	}
feed:
	for _, proj := range scan.projects {
		for _, art := range proj.artifacts {
			select {
			case work <- art:
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(work)
	wg.Wait()
	return ctx.Err()
}

// measureTree sums regular file sizes without following symlinks.
// unreadable reports entries that could not be read, which makes the size a
// lower bound and the tree one that cannot be proven removable whole.
func measureTree(ctx context.Context, dir string) (total, files int64, unreadable bool) {
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			unreadable = unreadable || !errors.Is(err, fs.ErrNotExist)
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			unreadable = unreadable || !errors.Is(infoErr, fs.ErrNotExist)
			return nil
		}
		total += info.Size()
		files++
		return nil
	})
	return total, files, unreadable
}

// sampleNewest returns the newest mtime among the first limit entries of dir
// in breadth-first order, skipping .git and artifact directories and never
// following symlinks. truncated reports that the limit cut the walk short.
func sampleNewest(ctx context.Context, dir string, limit int) (newest time.Time, truncated bool, err error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return time.Time{}, false, err
	}
	newest = info.ModTime()
	level := []string{dir}
	seen := 0
	for len(level) > 0 {
		var next []string
		for _, cur := range level {
			if err := ctx.Err(); err != nil {
				return newest, false, err
			}
			entries, readErr := os.ReadDir(cur)
			if readErr != nil {
				if errors.Is(readErr, fs.ErrNotExist) {
					continue
				}
				return newest, false, readErr
			}
			for _, e := range entries {
				if seen >= limit {
					return newest, true, nil
				}
				name := e.Name()
				if e.IsDir() && (neverDescend[name] || strings.HasPrefix(name, trashPrefix)) {
					continue
				}
				seen++
				fi, infoErr := e.Info()
				if infoErr != nil {
					continue
				}
				if fi.ModTime().After(newest) {
					newest = fi.ModTime()
				}
				if e.IsDir() {
					next = append(next, filepath.Join(cur, name))
				}
			}
		}
		level = next
	}
	return newest, false, nil
}
