package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/osshim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchProcess(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wanted []string
		want   string
	}{
		{"exact binary", "go build ./...", []string{"go"}, "go"},
		{"absolute path", "/usr/local/go/bin/go test ./...", []string{"go"}, "go"},
		{"second wanted", "/Users/me/.cargo/bin/rustc --crate-name x", []string{"cargo", "rustc"}, "rustc"},
		{"homebrew ruby script", "/opt/homebrew/ruby -W1 /opt/homebrew/Library/Homebrew/brew.rb cleanup", []string{"brew"}, "brew"},
		{"sudo wrapper", "sudo -u bob go build", []string{"go"}, "go"},
		{"env wrapper", "env -u FOO go test ./...", []string{"go"}, "go"},
		{"timeout wrapper", "timeout 60 go test", []string{"go"}, "go"},
		{"xargs", "xargs go vet", []string{"go"}, "go"},
		{"shell -c", `bash -lc "cargo build"`, []string{"cargo"}, "cargo"},
		{"shell chain", "/bin/sh -c cd /x && go build ./...", []string{"go"}, "go"},
		{"uvx belongs to uv", "/opt/homebrew/bin/uvx ruff check", []string{"uv"}, "uv"},
		{"editor arg errs toward busy", "vim go", []string{"go"}, "go"},
		{"prefix is not a match", "gopls serve", []string{"go"}, ""},
		{"similar name", "go-task build", []string{"go"}, ""},
		{"empty line", "", []string{"go"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchProcess(tt.line, tt.wanted))
		})
	}
}

func TestMatchProcess_WindowsImageNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("image-name suffix handling is windows-only")
	}
	assert.Equal(t, "go", matchProcess("go.exe", []string{"go"}))
	assert.Equal(t, "cargo", matchProcess(`C:\Tools\CARGO.EXE`, []string{"cargo"}))
}

func fakeGuard(lines []string, listErr error, held map[string]bool) *busyGuard {
	return &busyGuard{
		processes: []string{"cargo", "rustc"},
		listProcesses: func(context.Context) ([]string, error) {
			return lines, listErr
		},
		lockHeld: func(path string) (bool, error) {
			return held[path], nil
		},
	}
}

func TestBusyGuard_BusyReason(t *testing.T) {
	ctx := context.Background()

	t.Run("nil guard is never busy", func(t *testing.T) {
		var g *busyGuard
		assert.Empty(t, g.busyReason(ctx))
	})

	t.Run("process running", func(t *testing.T) {
		g := fakeGuard([]string{"zsh", "cargo build --release"}, nil, nil)
		assert.Equal(t, "cargo is running", g.busyReason(ctx))
	})

	t.Run("no matching process", func(t *testing.T) {
		g := fakeGuard([]string{"zsh", "vim main.go"}, nil, nil)
		assert.Empty(t, g.busyReason(ctx))
	})

	t.Run("lock held", func(t *testing.T) {
		g := fakeGuard(nil, nil, map[string]bool{"/c/.lock": true})
		g.locks = []string{"/c/.lock"}
		assert.Equal(t, "lock held: /c/.lock", g.busyReason(ctx))
	})

	t.Run("process list failure counts as busy", func(t *testing.T) {
		g := fakeGuard(nil, errors.New("ps denied"), nil)
		assert.Contains(t, g.busyReason(ctx), "cannot list processes: ps denied")
	})

	t.Run("lock check failure counts as busy", func(t *testing.T) {
		g := fakeGuard(nil, nil, nil)
		g.locks = []string{"/c/.lock"}
		g.lockHeld = func(string) (bool, error) { return false, errors.New("boom") }
		assert.Contains(t, g.busyReason(ctx), "cannot check lock /c/.lock: boom")
	})
}

func lockFile(t *testing.T, path string) {
	t.Helper()
	release, acquired, err := osshim.TryLock(path)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(release)
}

func newUVTestProvider(t *testing.T, cacheDir string) *FileProvider {
	t.Helper()
	p, err := NewFileProvider("uv", config.Provider{
		Enabled: true,
		Paths:   []string{cacheDir},
		MaxSize: "1",
	})
	require.NoError(t, err)
	return p
}

func TestFileProvider_SkipsWhenLockHeld(t *testing.T) {
	cacheDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "blob"), make([]byte, 64), 0o600))
	lockPath := filepath.Join(cacheDir, ".lock")
	lockFile(t, lockPath)

	p := newUVTestProvider(t, cacheDir)
	p.busy.listProcesses = func(context.Context) ([]string, error) { return nil, nil }

	for _, mode := range []CleanMode{CleanModeFull, CleanModeSmart} {
		result, err := p.Clean(context.Background(), CleanOptions{Mode: mode})
		require.NoError(t, err)
		assert.Equal(t, "lock held: "+lockPath, result.SkipReason)
		assert.Zero(t, result.BytesCleaned)
	}
	assert.FileExists(t, filepath.Join(cacheDir, "blob"), "busy provider must not delete files")
}

func TestFileProvider_CleansWhenIdle(t *testing.T) {
	cacheDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "blob"), make([]byte, 64), 0o600))

	p := newUVTestProvider(t, cacheDir)
	p.busy.listProcesses = func(context.Context) ([]string, error) { return []string{"zsh"}, nil }

	result, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeFull})
	require.NoError(t, err)
	assert.Empty(t, result.SkipReason)
	assert.NoFileExists(t, filepath.Join(cacheDir, "blob"))
}

func TestCommandProvider_SkipsWhenToolRunning(t *testing.T) {
	cacheDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	p, err := NewCommandProvider("go-build", config.Provider{
		Enabled:  true,
		Paths:    []string{cacheDir},
		MaxSize:  "1G",
		CleanCmd: "touch " + marker,
	})
	require.NoError(t, err)
	p.busy.listProcesses = func(context.Context) ([]string, error) {
		return []string{"/usr/local/go/bin/go build ./..."}, nil
	}

	result, err := p.Clean(context.Background(), CleanOptions{})
	require.NoError(t, err)
	assert.Equal(t, "go is running", result.SkipReason)
	assert.NoFileExists(t, marker, "busy provider must not run its clean command")

	dry, err := p.Clean(context.Background(), CleanOptions{DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, "go is running", dry.SkipReason)
}

func TestNewBusyGuard(t *testing.T) {
	assert.Nil(t, newBusyGuard("npm", []string{"/x"}))

	uv := newBusyGuard("uv", []string{"/a", "/b"})
	require.NotNil(t, uv)
	assert.Equal(t, []string{filepath.Join("/a", ".lock"), filepath.Join("/b", ".lock")}, uv.locks)
	assert.Equal(t, []string{"uv"}, uv.processes)

	cargo := newBusyGuard("cargo", nil)
	require.NotNil(t, cargo)
	assert.Equal(t, []string{"cargo", "rustc"}, cargo.processes)
}

func TestCommandProvider_CleanTimeout(t *testing.T) {
	p, err := NewCommandProvider("hang-test", config.Provider{
		Enabled:      true,
		Paths:        []string{t.TempDir()},
		MaxSize:      "1G",
		CleanCmd:     `sh -c "sleep 30"`,
		CleanTimeout: "1s",
	})
	require.NoError(t, err)

	start := time.Now()
	_, err = p.Clean(context.Background(), CleanOptions{})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after 1s")
	assert.Less(t, elapsed, 15*time.Second, "hung command must be cancelled")
}

func TestCommandProvider_TimeoutKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survivor")
	script := "(sleep 3; touch " + marker + ") & wait"
	p, err := NewCommandProvider("group-test", config.Provider{
		Enabled:      true,
		Paths:        []string{t.TempDir()},
		MaxSize:      "1G",
		CleanCmd:     "sh -c '" + script + "'",
		CleanTimeout: "1s",
	})
	require.NoError(t, err)

	_, err = p.Clean(context.Background(), CleanOptions{})
	require.Error(t, err)

	time.Sleep(3500 * time.Millisecond)
	assert.NoFileExists(t, marker, "grandchild must die with the timed-out command")
}

func TestCommandProvider_ParentCancelIsNotTimeout(t *testing.T) {
	p, err := NewCommandProvider("cancel-test", config.Provider{
		Enabled:  true,
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: `sh -c "sleep 30"`,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	_, err = p.Clean(ctx, CleanOptions{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "timed out")
}

func TestCommandProvider_TimeoutConfig(t *testing.T) {
	cfg := config.Provider{Enabled: true, Paths: []string{t.TempDir()}, MaxSize: "1G", CleanCmd: "true"}

	p, err := NewCommandProvider("t", cfg)
	require.NoError(t, err)
	assert.Equal(t, DefaultCleanTimeout, p.timeout)

	cfg.CleanTimeout = "90s"
	p, err = NewCommandProvider("t", cfg)
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, p.timeout)

	for _, bad := range []string{"0", "abc", " "} {
		cfg.CleanTimeout = bad
		_, err = NewCommandProvider("t", cfg)
		assert.Error(t, err, bad)
	}
}
