package auto

import (
	"os"
	"path/filepath"
	"strings"
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
	}
}

// protectedElement matches path elements that mark data auto must never
// touch, whichever provider points at it.
func protectedElement(part string) bool {
	return strings.EqualFold(part, "Downloads") || strings.EqualFold(part, "opencode") || strings.EqualFold(part, "worktrees")
}

// isProtected reports whether path is, is inside, or contains protected data.
func isProtected(path, home string) bool {
	candidates := []string{filepath.Clean(path)}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		candidates = append(candidates, resolved)
	}

	for _, p := range candidates {
		for part := range strings.SplitSeq(p, string(filepath.Separator)) {
			if protectedElement(part) {
				return true
			}
		}
		if containsProtectedDir(p) {
			return true
		}
		for _, root := range protectedRoots(home) {
			if within(strings.ToLower(p), strings.ToLower(root)) || within(strings.ToLower(root), strings.ToLower(p)) {
				return true
			}
		}
	}
	return false
}

// within reports whether path equals root or lies under it.
func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

const (
	descendantScanDepth   = 3
	descendantScanEntries = 20000
)

// containsProtectedDir reports whether a worktrees or opencode directory sits
// within a few levels below root, so a provider on an ancestor cannot reach
// into one. Downloads is left out: cache tools such as Homebrew keep a
// downloads directory of their own. The scan is bounded and never follows
// symlinks.
func containsProtectedDir(root string) bool {
	level := []string{root}
	scanned := 0
	for depth := 0; depth < descendantScanDepth && len(level) > 0; depth++ {
		var next []string
		for _, dir := range level {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				scanned++
				if scanned > descendantScanEntries {
					return false
				}
				if !e.IsDir() {
					continue
				}
				if strings.EqualFold(e.Name(), "worktrees") || strings.EqualFold(e.Name(), "opencode") {
					return true
				}
				next = append(next, filepath.Join(dir, e.Name()))
			}
		}
		level = next
	}
	return false
}
