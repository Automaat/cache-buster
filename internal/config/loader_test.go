package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadSaveCycle(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := NewLoader()
	loader.SetConfigPath(configPath)

	cfg := DefaultConfig()
	cfg.Providers["go-build"] = Provider{
		Enabled:  false,
		Paths:    []string{"~/custom/path"},
		MaxSize:  "20G",
		CleanCmd: "custom clean",
	}

	if err := loader.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	loader2 := NewLoader()
	loader2.SetConfigPath(configPath)

	loaded, err := loader2.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

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
}

func TestLoader_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := NewLoader()
	loader.SetConfigPath(configPath)

	exists, err := loader.Exists()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists {
		t.Error("Exists() = true, want false for missing file")
	}

	err = os.WriteFile(configPath, []byte("version: 1\nproviders: {}\n"), 0o600)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	exists, err = loader.Exists()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Error("Exists() = false, want true after file creation")
	}
}

func TestLoader_LoadOrCreate(t *testing.T) {
	t.Run("returns defaults when config missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		cfg, _, err := loader.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}
		if cfg == nil {
			t.Fatal("LoadOrCreate() config = nil")
		}
		if cfg.Version != "1" {
			t.Errorf("config.Version = %s, want 1", cfg.Version)
		}
		// Should have default providers
		if _, ok := cfg.Providers["go-build"]; !ok {
			t.Error("LoadOrCreate() missing default go-build provider")
		}
	})

	t.Run("merges user config with defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		// Write partial config with only one override
		content := `version: "1"
providers:
  custom:
    enabled: true
    paths:
      - /custom/path
    max_size: 5G
`
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		cfg, _, err := loader.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}
		// Should have custom provider
		if _, ok := cfg.Providers["custom"]; !ok {
			t.Error("LoadOrCreate() missing custom provider from user config")
		}
		// Should have default providers merged in
		if _, ok := cfg.Providers["go-build"]; !ok {
			t.Error("LoadOrCreate() missing default go-build provider after merge")
		}
	})
}

func TestLoader_InitDefault(t *testing.T) {
	t.Run("creates when missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		created, err := loader.InitDefault()
		if err != nil {
			t.Fatalf("InitDefault() error = %v", err)
		}
		if !created {
			t.Error("InitDefault() = false, want true")
		}

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config file not created")
		}
	})

	t.Run("skips when exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		if err := loader.Save(DefaultConfig()); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		created, err := loader.InitDefault()
		if err != nil {
			t.Fatalf("InitDefault() error = %v", err)
		}
		if created {
			t.Error("InitDefault() = true, want false for existing file")
		}
	})
}

func TestLoader_Save_ValidatesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loader := NewLoader()
	loader.SetConfigPath(configPath)

	cfg := &Config{
		Version: "1",
		Providers: map[string]Provider{
			"test": {Enabled: true, Paths: []string{}, MaxSize: "1G"}, // invalid: no paths
		},
	}

	err := loader.Save(cfg)
	if err == nil {
		t.Fatal("Save() expected error for invalid config")
	}
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
