package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Operating systems with their own default providers.
const (
	OSDarwin  = "darwin"
	OSLinux   = "linux"
	OSWindows = "windows"
)

// Platform is the OS and environment the default providers are built for.
// It is injectable so every OS is testable from any host. Paths below Home
// are written as ~/... so a saved config follows the user between machines.
type Platform struct {
	OS           string
	Home         string
	XDGCacheHome string
	XDGDataHome  string
	LocalAppData string
	TempDir      string
}

// CurrentPlatform describes the running OS and environment.
func CurrentPlatform() Platform {
	home, _ := os.UserHomeDir()
	return Platform{
		OS:           runtime.GOOS,
		Home:         home,
		XDGCacheHome: os.Getenv("XDG_CACHE_HOME"),
		XDGDataHome:  os.Getenv("XDG_DATA_HOME"),
		LocalAppData: os.Getenv("LOCALAPPDATA"),
		TempDir:      os.TempDir(),
	}
}

func (p Platform) home(rel string) string {
	return "~/" + rel
}

// cache spells a path below the OS cache root: ~/Library/Caches on macOS,
// $XDG_CACHE_HOME or ~/.cache on Linux, %LOCALAPPDATA% on Windows.
func (p Platform) cache(rel string) string {
	switch p.OS {
	case OSDarwin:
		return p.home("Library/Caches/" + rel)
	case OSWindows:
		return p.under(p.LocalAppData, "AppData/Local", rel)
	default:
		return p.under(p.XDGCacheHome, ".cache", rel)
	}
}

// data spells a path below the per-user data root: $XDG_DATA_HOME or
// ~/.local/share on Linux and macOS, %LOCALAPPDATA% on Windows.
func (p Platform) data(rel string) string {
	if p.OS == OSWindows {
		return p.under(p.LocalAppData, "AppData/Local", rel)
	}
	return p.under(p.XDGDataHome, ".local/share", rel)
}

// under joins rel to root, or to the home-relative fallback when root is
// unset. A root inside the home directory collapses to the ~/ spelling.
func (p Platform) under(root, fallback, rel string) string {
	if root == "" || !isAbsPortable(root) {
		return p.home(fallback + "/" + rel)
	}
	if sub, ok := cutDir(root, p.Home, p.OS == OSWindows); ok {
		return p.home(strings.TrimPrefix(sub+"/"+rel, "/"))
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// cutDir returns the slash-separated remainder of path below dir.
func cutDir(path, dir string, fold bool) (string, bool) {
	path = strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	dir = strings.TrimRight(strings.ReplaceAll(dir, `\`, "/"), "/")
	if dir == "" {
		return "", false
	}
	p, d := path, dir
	if fold {
		p, d = strings.ToLower(p), strings.ToLower(d)
	}
	if p == d {
		return "", true
	}
	if strings.HasPrefix(p, d+"/") {
		return path[len(dir)+1:], true
	}
	return "", false
}

func (p Platform) tempGlob(pattern string) string {
	dir := p.TempDir
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, pattern)
}

func hasDrive(path string) bool {
	return len(path) >= 2 && path[1] == ':' &&
		(path[0] >= 'a' && path[0] <= 'z' || path[0] >= 'A' && path[0] <= 'Z')
}

// isAbsPortable reports whether path is absolute on any supported OS, so a
// config written on one OS validates on another.
func isAbsPortable(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || filepath.IsAbs(path) {
		return true
	}
	return hasDrive(path) && len(path) > 2 && (path[2] == '/' || path[2] == '\\')
}

// providerOSes lists the OSes a built-in provider applies to. Providers not
// listed apply everywhere.
var providerOSes = map[string][]string{
	"homebrew":          {OSDarwin},
	"xcode-deriveddata": {OSDarwin},
	"xcode-archives":    {OSDarwin},
	"ios-simulator":     {OSDarwin},
	"vscode-shipit":     {OSDarwin},
	"lima":              {OSDarwin, OSLinux},
	"gh":                {OSDarwin, OSLinux},
}

// AppliesOn reports whether the named provider applies on goos. Custom
// providers apply everywhere.
func AppliesOn(name, goos string) bool {
	oses, ok := providerOSes[name]
	if !ok {
		return true
	}
	return slices.Contains(oses, goos)
}
