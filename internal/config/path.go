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

func ConfigDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, configDir), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

func EnsureConfigDir() error {
	dir, err := ConfigDirPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}
