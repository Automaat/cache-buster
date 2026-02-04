package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
)

func TestExpandPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		contains string
		patterns []string
		wantLen  int
	}{
		{
			name:     "plain path",
			patterns: []string{"/tmp"},
			wantLen:  1,
			contains: "/tmp",
		},
		{
			name:     "tilde expansion",
			patterns: []string{"~"},
			wantLen:  1,
			contains: home,
		},
		{
			name:     "tilde with path",
			patterns: []string{"~/.config"},
			wantLen:  1,
			contains: filepath.Join(home, ".config"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.ExpandPaths(tt.patterns)
			if err != nil {
				t.Fatalf("ExpandPaths() error = %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("ExpandPaths() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.contains != "" && (len(got) == 0 || got[0] != tt.contains) {
				t.Errorf("ExpandPaths() = %v, want to contain %q", got, tt.contains)
			}
		})
	}
}

func TestExpandPathsGlob(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"file1.txt", "file2.txt", "other.log"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := config.ExpandPaths([]string{filepath.Join(tmpDir, "*.txt")})
	if err != nil {
		t.Fatalf("ExpandPaths() error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ExpandPaths() len = %d, want 2", len(got))
	}
}

func TestCalculateSize(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]int{
		"file1.txt":          100,
		"file2.txt":          200,
		"subdir/file3.txt":   300,
		"subdir/nested/f.go": 400,
	}

	var expectedTotal int64
	for name, size := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		expectedTotal += int64(size)
	}

	got, err := CalculateSize([]string{tmpDir})
	if err != nil {
		t.Fatalf("CalculateSize() error = %v", err)
	}
	if got != expectedTotal {
		t.Errorf("CalculateSize() = %d, want %d", got, expectedTotal)
	}
}

func TestCalculateSizeMultiplePaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir1, "a.txt"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "b.txt"), make([]byte, 200), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := CalculateSize([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("CalculateSize() error = %v", err)
	}
	if got != 300 {
		t.Errorf("CalculateSize() = %d, want 300", got)
	}
}

func TestCalculateSizeSkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	realFile := filepath.Join(tmpDir, "real.txt")
	if err := os.WriteFile(realFile, make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}

	linkFile := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	got, err := CalculateSize([]string{tmpDir})
	if err != nil {
		t.Fatalf("CalculateSize() error = %v", err)
	}
	if got != 100 {
		t.Errorf("CalculateSize() = %d, want 100 (symlink should be skipped)", got)
	}
}

func TestListFiles(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]int{
		"file1.txt":        100,
		"file2.txt":        200,
		"subdir/file3.txt": 300,
	}

	for name, size := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ListFiles([]string{tmpDir})
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("ListFiles() len = %d, want 3", len(got))
	}

	sizeMap := make(map[string]int64)
	for _, fi := range got {
		sizeMap[filepath.Base(fi.Path)] = fi.Size
	}

	for name, wantSize := range files {
		base := filepath.Base(name)
		if gotSize := sizeMap[base]; gotSize != int64(wantSize) {
			t.Errorf("file %q size = %d, want %d", base, gotSize, wantSize)
		}
	}
}

func TestListFilesModTime(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	got, err := ListFiles([]string{tmpDir})
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListFiles() len = %d, want 1", len(got))
	}

	diff := now.Sub(got[0].ModTime)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Errorf("ModTime diff = %v, want < 1s", diff)
	}
}

func TestListFilesSkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	realFile := filepath.Join(tmpDir, "real.txt")
	if err := os.WriteFile(realFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	linkFile := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	got, err := ListFiles([]string{tmpDir})
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("ListFiles() len = %d, want 1 (symlink should be skipped)", len(got))
	}
}
