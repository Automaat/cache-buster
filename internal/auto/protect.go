package auto

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Automaat/cache-buster/internal/config"
)

// protectedRoots returns the home-relative locations auto never touches.
func protectedRoots(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "Downloads"),
		filepath.Join(home, ".local", "share", "opencode"),
		filepath.Join(home, ".config", "opencode"),
		filepath.Join(home, ".opencode"),
		filepath.Join(home, ".cache", "opencode"),
		filepath.Join(home, "Library", "Caches", "opencode"),
		filepath.Join(home, "Library", "Developer", "Xcode", "Archives"),
	}
}

// protectedElement matches path elements that mark data auto must never
// touch, whichever provider points at it.
func protectedElement(part string) bool {
	return strings.EqualFold(part, "Downloads") || strings.EqualFold(part, "opencode") || strings.EqualFold(part, "worktrees")
}

// isProtected reports whether path is, is inside, or contains protected data.
func isProtected(path, home string, scanTree bool) bool {
	return isProtectedWith(path, home, nil, scanTree)
}

// ProtectedPaths returns the absolute protected locations: the built-in
// defaults and the configured entries, with ~ expanded.
func ProtectedPaths(cfg *config.Config, home string) []string {
	var out []string
	for _, entry := range config.MergeProtected(cfg.Protected) {
		if rest, ok := strings.CutPrefix(entry, "~/"); ok {
			if home == "" {
				continue
			}
			entry = filepath.Join(home, rest)
		}
		if !filepath.IsAbs(entry) {
			continue
		}
		out = append(out, filepath.Clean(entry))
	}
	return out
}

// insideGitCheckout reports whether path is a git checkout or worktree root
// or lies inside one. The walk stops at home and the filesystem root, so a
// dotfiles repository in home does not protect everything below it.
func insideGitCheckout(path, home string) bool {
	var homes []string
	if home != "" {
		homes = withoutDataAlias([]string{filepath.Clean(home)})
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			homes = append(homes, withoutDataAlias([]string{resolved})...)
		}
	}
	isHome := func(dir string) bool {
		return slices.ContainsFunc(homes, func(h string) bool { return strings.EqualFold(h, dir) })
	}
	for _, start := range withoutDataAlias([]string{filepath.Clean(path)}) {
		for dir := start; ; dir = filepath.Dir(dir) {
			if dir == string(filepath.Separator) || dir == "." || isHome(dir) {
				break
			}
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
				return true
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return false
}

// isProtectedWith is isProtected plus extra protected roots, such as the
// configured protected list.
func isProtectedWith(path, home string, extra []string, scanTree bool) bool {
	candidates := []string{filepath.Clean(path)}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		candidates = append(candidates, resolved)
	}
	candidates = withoutDataAlias(candidates)

	var roots []string
	for _, root := range append(protectedRoots(home), extra...) {
		roots = append(roots, root)
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			roots = append(roots, resolved)
		}
	}
	roots = withoutDataAlias(roots)

	for _, p := range candidates {
		if p == string(filepath.Separator) || isXcodeArchives(p) || insideGitCheckout(p, home) {
			return true
		}
		for part := range strings.SplitSeq(p, string(filepath.Separator)) {
			if protectedElement(part) {
				return true
			}
		}
		if scanTree && containsProtectedDir(p) {
			return true
		}
		for _, root := range roots {
			if within(strings.ToLower(p), strings.ToLower(root)) || within(strings.ToLower(root), strings.ToLower(p)) {
				return true
			}
		}
	}
	return false
}

// dataVolumeAlias is the firmlink that exposes the user data volume under
// the same paths as the root filesystem.
const dataVolumeAlias = "/System/Volumes/Data"

// withoutDataAlias adds the alias-free spelling of every path under the
// data volume firmlink, so both spellings of home compare equal.
func withoutDataAlias(paths []string) []string {
	out := slices.Clone(paths)
	for _, p := range paths {
		if len(p) < len(dataVolumeAlias) || !strings.EqualFold(p[:len(dataVolumeAlias)], dataVolumeAlias) {
			continue
		}
		rest := p[len(dataVolumeAlias):]
		if rest == "" {
			out = append(out, string(filepath.Separator))
		} else if strings.HasPrefix(rest, string(filepath.Separator)) {
			out = append(out, rest)
		}
	}
	return out
}

// within reports whether path equals root or lies under it. The filesystem
// root contains every path.
func within(path, root string) bool {
	root = strings.TrimRight(root, string(filepath.Separator))
	return root == "" || path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

const descendantScanEntries = 500000

// containsProtectedDir reports whether a worktrees or opencode directory sits
// anywhere below root, so a provider on an ancestor cannot reach into one.
// Downloads matches only with its exact macOS capitalisation: cache tools
// such as Homebrew keep a lowercase downloads directory of their own. The scan never follows symlinks and fails closed
// once the tree is too large to verify.
func containsProtectedDir(root string) bool {
	level := []string{root}
	scanned := 0
	for len(level) > 0 {
		var next []string
		for _, dir := range level {
			entries, err := readDirUnsorted(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				scanned++
				if scanned > descendantScanEntries {
					return true
				}
				if !e.IsDir() {
					continue
				}
				if e.Name() == "Downloads" || strings.EqualFold(e.Name(), "worktrees") || strings.EqualFold(e.Name(), "opencode") {
					return true
				}
				next = append(next, filepath.Join(dir, e.Name()))
			}
		}
		level = next
	}
	return false
}

// readDirUnsorted lists dir without sorting; the scan does not need order.
func readDirUnsorted(dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadDir(-1)
}
