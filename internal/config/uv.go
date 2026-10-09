package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const uvProvider = "uv"

// ResolveUVCacheDir picks the directory bilgie sizes and hands to uv. The
// result is a literal path: callers must not glob-expand it again. An
// explicit path wins. The per-OS default follows uv's own order: UV_CACHE_DIR,
// then $XDG_CACHE_HOME/uv (not on Windows), then the OS default.
func ResolveUVCacheDir(paths []string) (string, error) {
	if slices.ContainsFunc(paths, func(p string) bool { return strings.TrimSpace(p) == "" }) {
		return "", fmt.Errorf("paths must not contain an empty entry")
	}
	expanded, err := ExpandPaths(paths)
	if err != nil {
		return "", fmt.Errorf("expand paths: %w", err)
	}
	if len(expanded) != 1 {
		return "", fmt.Errorf("paths must name exactly one uv cache directory, got %d", len(expanded))
	}
	dir := expanded[0]
	if !isDefaultUVDir(dir) {
		return dir, nil
	}
	if env := os.Getenv("UV_CACHE_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("resolve UV_CACHE_DIR %q: %w", env, err)
		}
		return abs, nil
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" && runtime.GOOS != "windows" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, uvProvider), nil
	}
	return dir, nil
}

func isDefaultUVDir(dir string) bool {
	defaults := []string{"~/.cache/uv"}
	defaults = append(defaults, DefaultProviders()[uvProvider].Paths...)
	for _, d := range defaults {
		exp, err := ExpandTilde(d)
		if err != nil {
			continue
		}
		if samePath(exp, dir) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ProviderPathsExist reports whether a provider has paths on disk. The uv
// cache directory is resolved first, so UV_CACHE_DIR counts even when the
// default directory is absent.
func ProviderPathsExist(name string, p Provider) bool {
	if name == uvProvider {
		dir, err := ResolveUVCacheDir(p.Paths)
		if err != nil {
			return true
		}
		_, statErr := os.Stat(dir)
		return statErr == nil
	}
	return PathsExist(p.Paths)
}
