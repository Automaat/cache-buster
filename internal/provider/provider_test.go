package provider_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
)

func TestBaseProvider(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Provider{
		Paths:   []string{tmpDir},
		MaxSize: "1G",
		Enabled: true,
	}

	base, err := provider.NewBaseProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if base.Name() != "test" {
		t.Errorf("name = %q, want %q", base.Name(), "test")
	}

	if len(base.Paths()) != 1 || base.Paths()[0] != tmpDir {
		t.Errorf("paths = %v, want [%s]", base.Paths(), tmpDir)
	}

	if base.MaxSize() != 1024*1024*1024 {
		t.Errorf("max size = %d, want %d", base.MaxSize(), 1024*1024*1024)
	}

	if !base.Available() {
		t.Error("available = false, want true")
	}

	size, err := base.CurrentSize()
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 {
		t.Errorf("current size = %d, want 5", size)
	}
}

func TestCommandProvider_DryRun(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "echo hello",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "would run: echo hello" {
		t.Errorf("output = %q, want %q", result.Output, "would run: echo hello")
	}
}

func TestCommandProvider_Clean(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "echo cleaned",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "cleaned" {
		t.Errorf("output = %q, want %q", result.Output, "cleaned")
	}
}

func TestFileProvider_AlreadyUnderLimit(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Provider{
		Paths:   []string{tmpDir},
		MaxSize: "1G",
		Enabled: true,
	}

	p, err := provider.NewFileProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "already under limit" {
		t.Errorf("output = %q, want %q", result.Output, "already under limit")
	}
}

func TestFileProvider_Clean(t *testing.T) {
	tmpDir := t.TempDir()

	oldFile := filepath.Join(tmpDir, "old.txt")
	newFile := filepath.Join(tmpDir, "new.txt")

	if err := os.WriteFile(oldFile, make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(newFile, make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Provider{
		Paths:   []string{tmpDir},
		MaxSize: "1000B",
		Enabled: true,
	}

	p, err := provider.NewFileProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesDeleted != 1 {
		t.Errorf("files deleted = %d, want 1", result.FilesDeleted)
	}

	if result.BytesCleaned != 1000 {
		t.Errorf("bytes cleaned = %d, want 1000", result.BytesCleaned)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old file should be deleted")
	}

	if _, err := os.Stat(newFile); err != nil {
		t.Error("new file should exist")
	}
}

func TestFileProvider_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, make([]byte, 2000), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Provider{
		Paths:   []string{tmpDir},
		MaxSize: "1000B",
		Enabled: true,
	}

	p, err := provider.NewFileProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesDeleted != 1 {
		t.Errorf("files deleted = %d, want 1", result.FilesDeleted)
	}

	if _, err := os.Stat(testFile); err != nil {
		t.Error("file should still exist after dry run")
	}
}

func TestDockerProvider_Available(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker system prune -af",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}
}

func TestNewProvider_CommandBased(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "10G",
		CleanCmd: "go clean -cache",
		Enabled:  true,
	}

	p, err := provider.NewProvider("go-build", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name() != "go-build" {
		t.Errorf("name = %q, want %q", p.Name(), "go-build")
	}
}

func TestNewProvider_FileBased(t *testing.T) {
	cfg := config.Provider{
		Paths:   []string{t.TempDir()},
		MaxSize: "4G",
		Enabled: true,
	}

	p, err := provider.NewProvider("uv", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name() != "uv" {
		t.Errorf("name = %q, want %q", p.Name(), "uv")
	}
}

func TestNewProvider_Docker(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker system prune -af",
		Enabled:  true,
	}

	p, err := provider.NewProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name() != "docker" {
		t.Errorf("name = %q, want %q", p.Name(), "docker")
	}
}

func TestLoadProviders(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Version: "1",
		Providers: map[string]config.Provider{
			"test1": {
				Paths:    []string{tmpDir},
				MaxSize:  "1G",
				CleanCmd: "echo 1",
				Enabled:  true,
			},
			"test2": {
				Paths:    []string{tmpDir},
				MaxSize:  "2G",
				CleanCmd: "echo 2",
				Enabled:  false,
			},
			"test3": {
				Paths:    []string{tmpDir},
				MaxSize:  "3G",
				CleanCmd: "echo 3",
				Enabled:  true,
			},
		},
	}

	providers, err := provider.LoadProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(providers) != 2 {
		t.Errorf("providers count = %d, want 2", len(providers))
	}
}

func TestLoadProvider(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Version: "1",
		Providers: map[string]config.Provider{
			"test": {
				Paths:    []string{tmpDir},
				MaxSize:  "1G",
				CleanCmd: "echo test",
				Enabled:  true,
			},
		},
	}

	p, err := provider.LoadProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name() != "test" {
		t.Errorf("name = %q, want %q", p.Name(), "test")
	}
}

func TestLoadProvider_NotFound(t *testing.T) {
	cfg := &config.Config{
		Version:   "1",
		Providers: map[string]config.Provider{},
	}

	_, err := provider.LoadProvider("nonexistent", cfg)
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestBaseProvider_InvalidMaxSize(t *testing.T) {
	cfg := config.Provider{
		Paths:   []string{t.TempDir()},
		MaxSize: "invalid",
		Enabled: true,
	}

	_, err := provider.NewBaseProvider("test", cfg)
	if err == nil {
		t.Error("expected error for invalid max size")
	}
}

func TestBaseProvider_InvalidPath(t *testing.T) {
	cfg := config.Provider{
		Paths:   []string{"~nonexistent[invalid"},
		MaxSize: "1G",
		Enabled: true,
	}

	_, err := provider.NewBaseProvider("test", cfg)
	if err == nil {
		t.Error("expected error for invalid path pattern")
	}
}

func TestCommandProvider_EmptyCmd(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.BytesCleaned != 0 {
		t.Errorf("bytes cleaned = %d, want 0", result.BytesCleaned)
	}
}

func TestCommandProvider_FailingCommand(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "false",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.Clean(context.Background(), provider.CleanOptions{})
	if err == nil {
		t.Error("expected error for failing command")
	}
}

func TestFileProvider_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	for i := range 10 {
		f := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(f, make([]byte, 1000), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Provider{
		Paths:   []string{tmpDir},
		MaxSize: "1B",
		Enabled: true,
	}

	p, err := provider.NewFileProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := p.Clean(ctx, provider.CleanOptions{})
	if err == nil {
		t.Error("expected context cancellation error")
	}

	if result.Output != "interrupted" {
		t.Errorf("output = %q, want %q", result.Output, "interrupted")
	}
}

func TestDockerProvider_CleanNotAvailable(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "nonexistent-docker-command",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Available() {
		t.Skip("docker is available, skipping unavailable test")
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "docker not available" {
		t.Errorf("output = %q, want %q", result.Output, "docker not available")
	}
}

func TestDockerProvider_DryRun(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker system prune -af",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "would run: docker system prune -af" {
		t.Errorf("output = %q, want %q", result.Output, "would run: docker system prune -af")
	}
}

func TestDockerProvider_EmptyCmd(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.BytesCleaned != 0 {
		t.Errorf("bytes cleaned = %d, want 0", result.BytesCleaned)
	}
}

func TestDockerProvider_FailingCommand(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker nonexistent-subcommand",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}

	_, err = p.Clean(context.Background(), provider.CleanOptions{})
	if err == nil {
		t.Error("expected error for failing command")
	}
}

func TestNewProvider_JetBrains(t *testing.T) {
	cfg := config.Provider{
		Paths:   []string{t.TempDir()},
		MaxSize: "3G",
		Enabled: true,
	}

	p, err := provider.NewProvider("jetbrains", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p.Name() != "jetbrains" {
		t.Errorf("name = %q, want %q", p.Name(), "jetbrains")
	}
}

func TestNewProvider_NoCleanCmd(t *testing.T) {
	cfg := config.Provider{
		Paths:   []string{t.TempDir()},
		MaxSize: "1G",
		Enabled: true,
	}

	_, err := provider.NewProvider("custom", cfg)
	if err == nil {
		t.Error("expected error for unknown provider without clean_cmd")
	}
}

func TestLoadProviders_Error(t *testing.T) {
	cfg := &config.Config{
		Version: "1",
		Providers: map[string]config.Provider{
			"bad": {
				Paths:   []string{t.TempDir()},
				MaxSize: "invalid",
				Enabled: true,
			},
		},
	}

	_, err := provider.LoadProviders(cfg)
	if err == nil {
		t.Error("expected error for invalid provider config")
	}
}

func TestCommandProvider_InvalidShellQuote(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "echo 'unterminated",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.Clean(context.Background(), provider.CleanOptions{})
	if err == nil {
		t.Error("expected error for invalid shell quote")
	}
}

func TestCommandProvider_QuotedArgs(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "1G",
		CleanCmd: "echo 'hello world'",
		Enabled:  true,
	}

	p, err := provider.NewCommandProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output != "hello world" {
		t.Errorf("output = %q, want %q", result.Output, "hello world")
	}
}

func TestDockerProvider_InvalidShellQuote(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker 'unterminated",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}

	_, err = p.Clean(context.Background(), provider.CleanOptions{})
	if err == nil {
		t.Error("expected error for invalid shell quote")
	}
}

func TestDockerProvider_CleanSuccess(t *testing.T) {
	cfg := config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "50G",
		CleanCmd: "docker version --format '{{.Server.Version}}'",
		Enabled:  true,
	}

	p, err := provider.NewDockerProvider("docker", cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Available() {
		t.Skip("docker not available")
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestFileProvider_DeleteError(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0o700); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(subDir, "test.txt")
	if err := os.WriteFile(testFile, make([]byte, 2000), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(subDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(subDir, 0o700)
	})

	cfg := config.Provider{
		Paths:   []string{subDir},
		MaxSize: "1000B",
		Enabled: true,
	}

	p, err := provider.NewFileProvider("test", cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := p.Clean(context.Background(), provider.CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesDeleted != 0 {
		t.Errorf("files deleted = %d, want 0", result.FilesDeleted)
	}

	if !strings.Contains(result.Output, "error") {
		t.Errorf("output should contain error info, got %q", result.Output)
	}
}
