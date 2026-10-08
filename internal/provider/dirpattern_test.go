package provider

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDirProvider(t *testing.T, root string, mutate func(*config.Provider)) *DirPatternProvider {
	t.Helper()

	cfg := config.Provider{
		Type:    config.TypeDirPattern,
		Paths:   []string{filepath.Join(root, "sail*")},
		MaxSize: "1G",
		Enabled: true,
	}
	if mutate != nil {
		mutate(&cfg)
	}

	p, err := NewDirPatternProvider("sail-test", cfg)
	require.NoError(t, err)
	p.openCheck = func(context.Context, string) (bool, error) { return false, nil }
	return p
}

func makeDir(t *testing.T, root, name string, n int, idle time.Duration) string {
	t.Helper()

	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "data.bin"), make([]byte, n), 0o600))
	ageTree(t, dir, idle)
	return dir
}

func ageTree(t *testing.T, dir string, idle time.Duration) {
	t.Helper()

	when := time.Now().Add(-idle)
	var paths []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}))
	for _, path := range slices.Backward(paths) {
		require.NoError(t, os.Chtimes(path, when, when))
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestDirPatternRemovesStaleDirectories(t *testing.T) {
	root := t.TempDir()
	stale := makeDir(t, root, "sail-a", 1000, 5*time.Hour)
	other := makeDir(t, root, "sail-b", 500, 3*time.Hour)
	unmatched := makeDir(t, root, "keep", 100, 10*time.Hour)

	p := newTestDirProvider(t, root, nil)
	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)

	assert.False(t, exists(stale))
	assert.False(t, exists(other))
	assert.True(t, exists(unmatched), "paths outside the glob must be untouched")
	assert.Equal(t, int64(1500), res.BytesCleaned)
	assert.Equal(t, int64(2), res.FilesDeleted)
	assert.Contains(t, res.Output, "removed: "+stale)
}

func TestDirPatternSkipRules(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, root string) string
		mutate     func(*config.Provider)
		openCheck  func(context.Context, string) (bool, error)
		wantReason string
	}{
		{
			name: "newer than min_idle",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				return makeDir(t, root, "sail-new", 10, 30*time.Minute)
			},
			wantReason: "min_idle",
		},
		{
			name: "fresh file deep in an old tree",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				dir := makeDir(t, root, "sail-deep", 10, 10*time.Hour)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "fresh.txt"), []byte("x"), 0o600))
				return dir
			},
			wantReason: "min_idle",
		},
		{
			name: "custom min_idle",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				return makeDir(t, root, "sail-custom", 10, 5*time.Hour)
			},
			mutate:     func(c *config.Provider) { c.MinIdle = "24h" },
			wantReason: "min_idle 24h0m0s",
		},
		{
			name: "open files",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				return makeDir(t, root, "sail-open", 10, 10*time.Hour)
			},
			openCheck:  func(context.Context, string) (bool, error) { return true, nil },
			wantReason: "has open files",
		},
		{
			name: "open check failure is conservative",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				return makeDir(t, root, "sail-lsof", 10, 10*time.Hour)
			},
			openCheck:  func(context.Context, string) (bool, error) { return false, errors.New("boom") },
			wantReason: "open-file check failed",
		},
		{
			name: ".git directory at top level",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				dir := makeDir(t, root, "sail-git", 10, 10*time.Hour)
				require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o750))
				ageTree(t, dir, 10*time.Hour)
				return dir
			},
			wantReason: "contains .git",
		},
		{
			name: ".git file (linked worktree) nested deeper",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				dir := makeDir(t, root, "sail-gitfile", 10, 10*time.Hour)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", ".git"), []byte("gitdir: x"), 0o600))
				ageTree(t, dir, 10*time.Hour)
				return dir
			},
			wantReason: "contains .git",
		},
		{
			name: "regular file matching the glob",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				f := filepath.Join(root, "sail-file")
				require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
				return f
			},
			wantReason: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			target := tt.setup(t, root)

			p := newTestDirProvider(t, root, tt.mutate)
			if tt.openCheck != nil {
				p.openCheck = tt.openCheck
			}

			res, err := p.Clean(t.Context(), CleanOptions{})
			require.NoError(t, err)

			assert.True(t, exists(target), "guarded path must survive")
			assert.Zero(t, res.BytesCleaned)
			assert.Contains(t, res.Output, "skip: "+target)
			assert.Contains(t, res.Output, tt.wantReason)
		})
	}
}

func TestDirPatternGuardsCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	dir := makeDir(t, root, "sail-git", 10, 10*time.Hour)
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o750))
	ageTree(t, dir, 10*time.Hour)

	off := false
	p := newTestDirProvider(t, root, func(c *config.Provider) {
		c.SkipIfGitWorktree = &off
		c.SkipIfOpen = &off
	})
	p.openCheck = func(context.Context, string) (bool, error) { return true, nil }

	_, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.False(t, exists(dir))
}

func TestDirPatternSymlinkIsNeverFollowed(t *testing.T) {
	root := t.TempDir()
	outside := makeDir(t, t.TempDir(), "precious", 10, 10*time.Hour)
	link := filepath.Join(root, "sail-link")
	require.NoError(t, os.Symlink(outside, link))

	inner := makeDir(t, root, "sail-inner", 10, 10*time.Hour)
	require.NoError(t, os.Symlink(outside, filepath.Join(inner, "escape")))
	ageTree(t, inner, 10*time.Hour)

	p := newTestDirProvider(t, root, func(c *config.Provider) { c.MinIdle = "0s" })
	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)

	assert.True(t, exists(link), "top-level symlink is skipped")
	assert.Contains(t, res.Output, "skip: "+link+" (symlink)")
	assert.False(t, exists(inner))
	assert.True(t, exists(filepath.Join(outside, "sub", "data.bin")), "symlink target must survive")
	assert.Equal(t, int64(10), res.BytesCleaned)
}

func TestDirPatternDryRunDeletesNothing(t *testing.T) {
	root := t.TempDir()
	stale := makeDir(t, root, "sail-stale", 2048, 5*time.Hour)
	fresh := makeDir(t, root, "sail-fresh", 10, time.Minute)

	p := newTestDirProvider(t, root, nil)
	res, err := p.Clean(t.Context(), CleanOptions{DryRun: true})
	require.NoError(t, err)

	assert.True(t, exists(stale))
	assert.True(t, exists(fresh))
	assert.Equal(t, int64(2048), res.BytesCleaned)
	assert.Contains(t, res.Output, "would remove: "+stale)
	assert.Contains(t, res.Output, "2.0 KiB")
	assert.Contains(t, res.Output, "idle 5h0m")
	assert.Contains(t, res.Output, "skip: "+fresh)
	assert.Contains(t, res.Output, "min_idle")
}

func TestDirPatternSmartModeRemovesWholeDirectories(t *testing.T) {
	root := t.TempDir()
	stale := makeDir(t, root, "sail-a", 100, 5*time.Hour)

	p := newTestDirProvider(t, root, nil)
	_, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)

	assert.False(t, exists(stale), "no empty directory may be left behind")
}

func TestDirPatternProtectedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(home, "sail*")}, MaxSize: "1G",
	})
	require.NoError(t, err)

	assert.True(t, p.isProtected(home))
	assert.True(t, p.isProtected(filepath.Dir(home)))
	assert.True(t, p.isProtected(string(filepath.Separator)))
	assert.True(t, p.isProtected(filepath.Join(home, "Documents")))
	assert.True(t, p.isProtected("/Applications"))
	assert.False(t, p.isProtected(filepath.Join(home, "work", "sail-a")))
	assert.False(t, p.isProtected("/private/tmp/sail-a"))
}

func TestDirPatternProtectedMatchIsSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(home, "sail*")}, MaxSize: "1G", MinIdle: "0s",
	})
	require.NoError(t, err)
	p.paths = []string{home}
	p.openCheck = func(context.Context, string) (bool, error) { return false, nil }

	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.Contains(t, res.Output, "protected path")
	assert.True(t, exists(home))
}

func TestDirPatternUnreadableSubtreeIsSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}

	root := t.TempDir()
	dir := makeDir(t, root, "sail-locked", 10, 10*time.Hour)
	locked := filepath.Join(dir, "sub")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	p := newTestDirProvider(t, root, nil)
	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)

	assert.True(t, exists(dir))
	assert.Contains(t, res.Output, "scan failed")
}

func TestDirPatternCancelledContext(t *testing.T) {
	root := t.TempDir()
	dir := makeDir(t, root, "sail-a", 10, 5*time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	p := newTestDirProvider(t, root, nil)
	_, err := p.Clean(ctx, CleanOptions{})
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, exists(dir))
}

func TestNewDirPatternProviderValidation(t *testing.T) {
	_, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{"/nonexistent-cb/sail*"}, MaxSize: "1G", MinIdle: "soon",
	})
	require.ErrorContains(t, err, "min_idle")

	p, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{"/nonexistent-cb/sail*"}, MaxSize: "1G",
	})
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, p.minIdle, "default min_idle")
	assert.True(t, p.skipIfOpen)
	assert.True(t, p.skipIfGitWorktree)
	assert.True(t, p.Available())
}

func TestNewProviderSelectsDirPattern(t *testing.T) {
	p, err := NewProvider("sail-dirs", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{"/nonexistent-cb/sail*"}, MaxSize: "1G",
	})
	require.NoError(t, err)
	assert.IsType(t, &DirPatternProvider{}, p)
}

func TestDefaultSailProviderIsOptIn(t *testing.T) {
	cfg := config.DefaultConfig()
	sail, ok := cfg.GetProvider("sail-dirs")
	require.True(t, ok)

	assert.False(t, sail.Enabled, "destructive provider must be opt-in")
	assert.Equal(t, []string{"/private/tmp/sail*"}, sail.Paths)
	assert.Equal(t, config.TypeDirPattern, sail.Type)
	assert.NotContains(t, cfg.AllEnabledProviders(), "sail-dirs")
}

func TestLsofHasOpenFiles(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not installed")
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "held.txt"), []byte("x"), 0o600))

	open, err := lsofHasOpenFiles(t.Context(), dir)
	require.NoError(t, err)
	assert.False(t, open, "no process holds a file yet")

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	f, err := root.Open("held.txt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	open, err = lsofHasOpenFiles(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, open)
}

func TestDirPatternActivityDuringChecksBlocksRemoval(t *testing.T) {
	root := t.TempDir()
	dir := makeDir(t, root, "sail-busy", 10, 10*time.Hour)

	p := newTestDirProvider(t, root, nil)
	p.openCheck = func(context.Context, string) (bool, error) {
		return false, os.WriteFile(filepath.Join(dir, "sub", "late.txt"), []byte("x"), 0o600)
	}

	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.True(t, exists(dir))
	assert.Contains(t, res.Output, "modified during checks")
}

func TestDirPatternGitAppearingDuringChecksBlocksRemoval(t *testing.T) {
	root := t.TempDir()
	dir := makeDir(t, root, "sail-late-git", 10, 10*time.Hour)

	p := newTestDirProvider(t, root, nil)
	p.openCheck = func(context.Context, string) (bool, error) {
		return false, os.Mkdir(filepath.Join(dir, ".git"), 0o750)
	}

	_, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.True(t, exists(dir))
}

func TestNewDirPatternProviderRejectsLiteralPaths(t *testing.T) {
	_, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{t.TempDir()}, MaxSize: "1G",
	})
	require.ErrorContains(t, err, "must contain a glob")
}

func TestNewDirPatternProviderRejectsRelativePaths(t *testing.T) {
	for _, path := range []string{"*", "Documents/*", "-x*", "./sail*"} {
		_, err := NewDirPatternProvider("x", config.Provider{
			Type: config.TypeDirPattern, Paths: []string{path}, MaxSize: "1G",
		})
		require.ErrorContains(t, err, "absolute", path)
	}
}

func TestDirPatternRelativeMatchIsProtected(t *testing.T) {
	p := newTestDirProvider(t, t.TempDir(), nil)
	assert.True(t, p.isProtected("Documents"))
}

func TestDirPatternProtectsHomeReachedThroughSymlink(t *testing.T) {
	sandbox := t.TempDir()
	home := filepath.Join(sandbox, "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	t.Setenv("HOME", home)
	require.NoError(t, os.Symlink(home, filepath.Join(sandbox, "homelink")))
	victim := makeDir(t, home, "sail-home", 10, 10*time.Hour)

	p, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(sandbox, "homelink", "sail*")}, MaxSize: "1G",
	})
	require.NoError(t, err)
	p.openCheck = func(context.Context, string) (bool, error) { return false, nil }

	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.True(t, exists(victim))
	assert.Contains(t, res.Output, "protected path")
}

func TestDirPatternNeverRemovesGitDirectoryItself(t *testing.T) {
	root := t.TempDir()
	repo := makeDir(t, root, "repo", 10, 10*time.Hour)
	gitDir := filepath.Join(repo, ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "objects"), 0o750))
	ageTree(t, repo, 10*time.Hour)

	p, err := NewDirPatternProvider("x", config.Provider{
		Type: config.TypeDirPattern, Paths: []string{filepath.Join(root, "*", ".git*")}, MaxSize: "1G",
	})
	require.NoError(t, err)
	p.openCheck = func(context.Context, string) (bool, error) { return false, nil }

	res, err := p.Clean(t.Context(), CleanOptions{})
	require.NoError(t, err)
	assert.True(t, exists(gitDir))
	assert.Contains(t, res.Output, "is a .git directory")
}
