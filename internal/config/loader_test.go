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
	t.Run("creates default when missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		cfg, created, err := loader.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}
		if !created {
			t.Error("LoadOrCreate() created = false, want true")
		}
		if cfg == nil {
			t.Fatal("LoadOrCreate() config = nil")
		}
		if cfg.Version != "1" {
			t.Errorf("config.Version = %s, want 1", cfg.Version)
		}

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config file not created on disk")
		}
	})

	t.Run("loads existing config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		original := DefaultConfig()
		original.Providers["test"] = Provider{
			Enabled:  true,
			Paths:    []string{"/test"},
			MaxSize:  "5G",
			CleanCmd: "rm -rf /test",
		}
		if err := loader.Save(original); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loader2 := NewLoader()
		loader2.SetConfigPath(configPath)

		cfg, created, err := loader2.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}
		if created {
			t.Error("LoadOrCreate() created = true, want false for existing")
		}
		if _, ok := cfg.Providers["test"]; !ok {
			t.Error("loaded config missing test provider")
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
		Version:   "", // invalid version
		Providers: map[string]Provider{},
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
