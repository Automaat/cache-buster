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

	// Config file should NOT be auto-created (uses defaults in memory)
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should not be auto-created")
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

func TestDefaultDriftNotes(t *testing.T) {
	cfg := config.DefaultConfig()
	if notes := defaultDriftNotes(cfg); len(notes) != 0 {
		t.Fatalf("defaults must not drift, got %v", notes)
	}

	hf := cfg.Providers["huggingface"]
	hf.Enabled = !hf.Enabled
	cfg.Providers["huggingface"] = hf
	cfg.Providers["custom-tool"] = config.Provider{Enabled: true}

	notes := defaultDriftNotes(cfg)
	want := "# note: huggingface has enabled: true saved; the current default is false"
	if len(notes) != 1 || notes[0] != want {
		t.Fatalf("notes = %v, want [%q]", notes, want)
	}
}
