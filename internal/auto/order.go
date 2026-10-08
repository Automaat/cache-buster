package auto

import (
	"cmp"
	"slices"
)

// rebuildRank orders providers by how costly their cache is to recreate:
// lower ranks are re-downloaded cheaply, higher ranks need long rebuilds or
// hold artifacts that are hard to replace.
var rebuildRank = map[string]int{
	"pip":               10,
	"npm":               10,
	"yarn":              10,
	"pnpm":              10,
	"homebrew":          15,
	"ios-simulator":     20,
	"jetbrains":         20,
	"uv":                25,
	"mise":              30,
	"cargo":             40,
	"gradle":            40,
	"go-mod":            45,
	"go-build":          50,
	"xcode-deriveddata": 55,
	"project-artifacts": 60,
	"docker":            70,
	"xcode-archives":    90,
}

// unknownRank places providers without an entry after the cheap downloads.
const unknownRank = 35

func rankOf(name string) int {
	if r, ok := rebuildRank[name]; ok {
		return r
	}
	return unknownRank
}

func sortByRebuildCost(names []string) {
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(rankOf(a), rankOf(b)), cmp.Compare(a, b))
	})
}
