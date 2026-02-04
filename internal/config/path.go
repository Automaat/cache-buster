package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDir  = ".config/cache-buster"
	configFile = "config.yaml"
)

// ExpandTilde replaces ~ prefix with home directory.
func ExpandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}

	return path, nil
}

// ExpandPaths expands ~ and globs in path patterns.
func ExpandPaths(patterns []string) ([]string, error) {
	var result []string

	for _, pattern := range patterns {
		expanded, err := ExpandTilde(pattern)
		if err != nil {
			return nil, err
		}

		if strings.ContainsAny(expanded, "*?[") {
			matches, err := filepath.Glob(expanded)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", pattern, err)
			}
			result = append(result, matches...)
		} else {
			result = append(result, expanded)
		}
	}

	return result, nil
}

// DirPath returns ~/.config/cache-buster.
func DirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, configDir), nil
}

// Path returns ~/.config/cache-buster/config.yaml.
func Path() (string, error) {
	dir, err := DirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

// EnsureDir creates config directory if missing.
func EnsureDir() error {
	dir, err := DirPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o750)
}

// PathsExist checks if any of the given paths exist on disk.
func PathsExist(patterns []string) bool {
	paths, err := ExpandPaths(patterns)
	if err != nil {
		return false
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
