package auto

import (
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
	}
}

// protectedElement matches path elements that mark data auto must never
// touch, whichever provider points at it.
func protectedElement(part string) bool {
	return part == "Downloads" || strings.EqualFold(part, "opencode") || strings.EqualFold(part, "worktrees")
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
		for _, root := range protectedRoots(home) {
			if within(p, root) || within(root, p) {
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
