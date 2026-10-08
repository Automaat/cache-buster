package provider

import "strings"

// IsGitMarkerName reports whether a directory entry name is a git checkout
// marker. The match ignores case so a .GIT entry on a case-sensitive volume
// protects as it does on a case-insensitive one: fail closed on every OS.
func IsGitMarkerName(name string) bool {
	return strings.EqualFold(name, ".git")
}
