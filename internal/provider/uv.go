package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/smykla-skalski/bilgie/internal/config"
)

const (
	uvBinary          = "uv"
	uvDefaultCleanCmd = "uv cache clean"
	uvSmartCleanCmd   = "uv cache prune"
)

// UVProvider cleans the uv cache with uv itself. Files inside the cache
// reference each other (the wheels-v* pointers name archive-v0/<id>), so
// deleting any of them breaks the next install; only uv may remove them.
// bilgie never deletes below the cache directory. Smart mode runs
// `uv cache prune` (unused entries), full mode runs `uv cache clean`.
type UVProvider struct {
	*BaseProvider
	lookPath func(string) (string, error)
	cleanCmd string
	fullArgs []string
	timeout  time.Duration

	mu        sync.Mutex
	protected []string
}

// NewUVProvider creates the uv provider. clean_cmd is optional and, when
// set, must be `uv cache clean`, `uv cache prune` or `uv cache prune --ci`;
// it replaces the full-mode command. paths must hold exactly one directory.
func NewUVProvider(name string, cfg config.Provider) (*UVProvider, error) {
	cmd := strings.TrimSpace(cfg.CleanCmd)
	if cmd == "" {
		cmd = uvDefaultCleanCmd
	}
	fullArgs, err := parseUVCleanCmd(cmd)
	if err != nil {
		return nil, err
	}
	timeout, err := parseCleanTimeout(cfg.CleanTimeout)
	if err != nil {
		return nil, err
	}
	dir, err := resolveUVCacheDir(cfg.Paths)
	if err != nil {
		return nil, err
	}
	cfg.Paths = []string{dir}
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	p := &UVProvider{
		BaseProvider: base,
		lookPath:     exec.LookPath,
		cleanCmd:     cmd,
		fullArgs:     fullArgs,
		timeout:      timeout,
	}
	p.protected = expandProtected(config.DefaultProtected(), home)
	p.protected = append(p.protected, config.BuiltinProtectedRoots(home)...)
	return p, nil
}

func parseUVCleanCmd(cmd string) ([]string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return nil, fmt.Errorf("invalid clean_cmd: %w", err)
	}
	ok := len(words) >= 3 && words[0] == uvBinary && words[1] == "cache"
	switch {
	case ok && words[2] == "clean":
		ok = len(words) == 3
	case ok && words[2] == "prune":
		ok = len(words) == 3 || (len(words) == 4 && words[3] == "--ci")
	default:
		ok = false
	}
	if !ok {
		return nil, fmt.Errorf(
			`clean_cmd must be "uv cache clean", "uv cache prune" or "uv cache prune --ci" `+
				`(bilgie runs uv itself and never deletes files in the uv cache), got %q; `+
				`remove clean_cmd to use the default`, cmd)
	}
	return words[1:], nil
}

// resolveUVCacheDir picks the directory bilgie sizes and hands to uv. An
// explicit path wins. The per-OS default follows uv's own order: UV_CACHE_DIR,
// then $XDG_CACHE_HOME/uv (not on Windows), then the OS default.
func resolveUVCacheDir(paths []string) (string, error) {
	expanded, err := config.ExpandPaths(paths)
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
		return filepath.Join(xdg, uvBinary), nil
	}
	return dir, nil
}

func isDefaultUVDir(dir string) bool {
	defaults := []string{"~/.cache/uv"}
	defaults = append(defaults, config.DefaultProviders()[uvBinary].Paths...)
	for _, d := range defaults {
		exp, err := config.ExpandTilde(d)
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

// SetProtected implements ProtectionAware. The paths add to the built-in ones.
func (p *UVProvider) SetProtected(paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, path := range paths {
		if path = filepath.Clean(path); !slices.Contains(p.protected, path) {
			p.protected = append(p.protected, path)
		}
	}
}

func (p *UVProvider) findUV() (string, bool) {
	bin, err := p.lookPath(uvBinary)
	return bin, err == nil
}

// Available reports whether uv is on PATH.
func (p *UVProvider) Available() bool {
	_, found := p.findUV()
	return found
}

// Clean implements Provider.
func (p *UVProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	if reason := p.guardReason(); reason != "" {
		return skipResult(reason), nil
	}
	if skipped, ok := p.skipIfBusy(ctx); ok {
		return skipped, nil
	}
	bin, found := p.findUV()
	if !found {
		return skipResult("uv not found on PATH"), nil
	}

	args, display := p.fullArgs, p.cleanCmd
	if opts.Mode == CleanModeSmart {
		args, display = []string{"cache", "prune"}, uvSmartCleanCmd
	}

	if opts.DryRun {
		cacheSize := "size unknown"
		if current, sizeErr := p.CurrentSize(ctx); sizeErr == nil {
			cacheSize = sizeText(current)
		}
		return CleanResult{
			Output: fmt.Sprintf("would run: %s (cache %s, uv has no dry-run)", display, cacheSize),
		}, nil
	}

	res, err := runMeasuredCleanEnv(ctx, p.name, append([]string{bin}, args...), p.childEnv(), p.CurrentSize, p.timeout)
	if err != nil {
		if ctx.Err() != nil {
			return CleanResult{Output: res.Output}, ctx.Err()
		}
		reason := fmt.Sprintf("%s failed: %v", display, err)
		if line := firstLine(res.Output); line != "" {
			reason += ": " + line
		}
		return skipResult(reason), nil
	}
	return res, nil
}

// childEnv pins uv to the directory bilgie measured and drops UV_NO_CACHE,
// which would send uv to a throwaway directory.
func (p *UVProvider) childEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(key, "UV_CACHE_DIR") || strings.EqualFold(key, "UV_NO_CACHE") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "UV_CACHE_DIR="+p.paths[0])
}

// guardReason refuses a cache directory that is, lies inside or contains a
// protected location: uv cache clean empties whatever directory it is given.
func (p *UVProvider) guardReason() string {
	p.mu.Lock()
	protected := slices.Clone(p.protected)
	p.mu.Unlock()
	for _, cand := range pathSpellings(p.paths[0]) {
		if lexicallyProtected(cand) {
			return "protected path " + p.paths[0]
		}
		for _, root := range protected {
			for _, spelling := range rootSpellings(root) {
				if pathWithin(cand, spelling) || pathWithin(spelling, cand) {
					return "protected path " + root
				}
			}
		}
	}
	return ""
}

var _ ProtectionAware = (*UVProvider)(nil)
