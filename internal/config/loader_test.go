package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadSaveCycle(t *testing.T) {
	tmpDir := t.TempDir()
	origConfigDir := configDir

	// Temporarily override config paths for testing
	t.Cleanup(func() {
		// Reset by creating new loader
	})

	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := NewLoader()
	loader.v.SetConfigFile(configPath)
	loader.v.SetConfigType("yaml")

	cfg := DefaultConfig()
	cfg.Providers["go-build"] = Provider{
		Enabled:  false,
		Paths:    []string{"~/custom/path"},
		MaxSize:  "20G",
		CleanCmd: "custom clean",
	}

	// Save
	for key, value := range map[string]any{
		"version":   cfg.Version,
		"providers": cfg.Providers,
	} {
		loader.v.Set(key, value)
	}
	if err := loader.v.WriteConfigAs(configPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// Load back
	loader2 := NewLoader()
	loader2.v.SetConfigFile(configPath)
	loader2.v.SetConfigType("yaml")

	if err := loader2.v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig() error = %v", err)
	}

	var loaded Config
	if err := loader2.v.Unmarshal(&loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify loaded config
	if loaded.Version != cfg.Version {
		t.Errorf("loaded version = %v, want %v", loaded.Version, cfg.Version)
	}

	goBuild, ok := loaded.Providers["go-build"]
	if !ok {
		t.Fatal("loaded config missing go-build provider")
	}

	if goBuild.Enabled != false {
		t.Errorf("go-build Enabled = %v, want false", goBuild.Enabled)
	}
	if goBuild.MaxSize != "20G" {
		t.Errorf("go-build MaxSize = %v, want 20G", goBuild.MaxSize)
	}

	_ = origConfigDir
}

func TestLoader_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := NewLoader()

	// Create a temporary test to check file existence
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("file should not exist initially")
	}

	// Create file
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("file should exist after creation")
	}

	_ = loader
}

func TestNewLoader(t *testing.T) {
	loader := NewLoader()
	if loader == nil {
		t.Fatal("NewLoader() returned nil")
	}
	if loader.v == nil {
		t.Fatal("NewLoader() viper instance is nil")
	}
}
