package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"tilde only", "~", home, false},
		{"tilde with path", "~/foo/bar", filepath.Join(home, "foo/bar"), false},
		{"no tilde", "/absolute/path", "/absolute/path", false},
		{"relative path", "relative/path", "relative/path", false},
		{"tilde in middle", "/path/~user", "/path/~user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTilde(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandTilde() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ExpandTilde() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("create test file: %v", err)
	}

	tests := []struct {
		contains string
		name     string
		patterns []string
		wantLen  int
	}{
		{filepath.Join(home, "foo"), "tilde expansion", []string{"~/foo"}, 1},
		{testFile, "glob match", []string{filepath.Join(tmpDir, "*.txt")}, 1},
		{"", "no glob match", []string{filepath.Join(tmpDir, "*.json")}, 0},
		{testFile, "mixed", []string{"~/foo", filepath.Join(tmpDir, "*.txt")}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandPaths(tt.patterns)
			if err != nil {
				t.Errorf("ExpandPaths() error = %v", err)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("ExpandPaths() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.contains != "" {
				found := false
				for _, p := range got {
					if p == tt.contains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("ExpandPaths() missing %v in %v", tt.contains, got)
				}
			}
		})
	}
}

func TestPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}

	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	want := filepath.Join(home, ".config/cache-buster/config.yaml")
	if path != want {
		t.Errorf("Path() = %v, want %v", path, want)
	}
}

func TestDirPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("get home dir: %v", err)
	}

	path, err := DirPath()
	if err != nil {
		t.Fatalf("DirPath() error = %v", err)
	}

	want := filepath.Join(home, ".config/cache-buster")
	if path != want {
		t.Errorf("DirPath() = %v, want %v", path, want)
	}
}
