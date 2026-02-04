package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
)

func TestConfigShow(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := config.NewLoader()
	loader.SetConfigPath(configPath)

	err := runConfigShowWithLoader(loader)
	if err != nil {
		t.Fatalf("runConfigShowWithLoader failed: %v", err)
	}

	// Verify config was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created")
	}
}

func TestConfigInit_New(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := config.NewLoader()
	loader.SetConfigPath(configPath)

	err := runConfigInitWithLoader(loader)
	if err != nil {
		t.Fatalf("runConfigInitWithLoader failed: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created")
	}
}

func TestConfigInit_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := config.NewLoader()
	loader.SetConfigPath(configPath)

	// Create first
	if _, err := loader.InitDefault(); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	// Run again - should not error
	err := runConfigInitWithLoader(loader)
	if err != nil {
		t.Fatalf("runConfigInitWithLoader failed on existing: %v", err)
	}
}

func TestConfigEdit_NoEditor(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := config.NewLoader()
	loader.SetConfigPath(configPath)

	// Test with non-existent editor (to fail fast)
	err := runConfigEditWithLoader(loader, "nonexistent-editor-abc123")
	if err == nil {
		t.Error("expected error with non-existent editor")
	}

	// Config should still be created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created before edit")
	}
}
