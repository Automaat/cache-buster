package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	t.Run("override max_size only keeps provider enabled with default paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		content := "version: \"1\"\nproviders:\n  go-build:\n    max_size: 20G\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		loader := NewLoader()
		loader.SetConfigPath(configPath)

		cfg, _, err := loader.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}

		goBuild, ok := cfg.Providers["go-build"]
		if !ok {
			t.Fatal("go-build provider missing")
		}
		if !goBuild.Enabled {
			t.Error("go-build should remain enabled when only max_size is overridden")
		}
		if goBuild.MaxSize != "20G" {
			t.Errorf("go-build MaxSize = %v, want 20G", goBuild.MaxSize)
		}
		defaults := DefaultProviders()
		defaultGoBuild := defaults["go-build"]
		if len(goBuild.Paths) != len(defaultGoBuild.Paths) {
			t.Errorf("go-build Paths length = %d, want %d (default paths)", len(goBuild.Paths), len(defaultGoBuild.Paths))
		} else {
			for i, p := range defaultGoBuild.Paths {
				if goBuild.Paths[i] != p {
					t.Errorf("go-build Paths[%d] = %q, want %q (default paths %v)", i, goBuild.Paths[i], p, defaultGoBuild.Paths)
					break
				}
			}
		}
	})

	t.Run("user can disable default provider", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		content := `version: "1"
providers:
  go-build:
    enabled: false
    paths:
      - ~/Library/Caches/go-build
    max_size: 10G
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

		goBuild, ok := cfg.Providers["go-build"]
		if !ok {
			t.Fatal("go-build provider missing")
		}
		if goBuild.Enabled {
			t.Error("user override enabled=false not applied")
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

func TestLoader_SkipPrefixesAreUnioned(t *testing.T) {
	for _, list := range []string{"[]", `[""]`, "[extra-]"} {
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		content := "version: \"1\"\nproviders:\n  chrome-devtools-mcp:\n    enabled: true\n    skip_prefixes: " + list + "\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		loader := NewLoader()
		loader.SetConfigPath(configPath)

		cfg, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		p := cfg.Providers["chrome-devtools-mcp"]
		if !slices.Contains(p.SkipPrefixes, "chrome-profile-") {
			t.Errorf("skip_prefixes %s dropped chrome-profile-: %v", list, p.SkipPrefixes)
		}
		if list == "[extra-]" && !slices.Contains(p.SkipPrefixes, "extra-") {
			t.Errorf("user prefix lost: %v", p.SkipPrefixes)
		}
	}
}

func TestLoader_ProtectedIsUnionWithDefaults(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
		not  []string
	}{
		{"absent", "version: \"1\"\n", DefaultProtected(), nil},
		{"empty list keeps defaults", "version: \"1\"\nprotected: []\n", DefaultProtected(), nil},
		{"user entries added", "version: \"1\"\nprotected:\n  - ~/keep\n  - ~/Downloads\n", append(DefaultProtected(), "~/keep"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			loader := NewLoader()
			loader.SetConfigPath(path)

			cfg, err := loader.Load()

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Protected)
		})
	}
}

func TestLoader_SaveKeepsUserProtectedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	loader := NewLoader()
	loader.SetConfigPath(path)
	cfg := DefaultConfig()
	cfg.Protected = MergeProtected([]string{"~/keep"})
	require.NoError(t, loader.Save(cfg))

	reloader := NewLoader()
	reloader.SetConfigPath(path)
	got, err := reloader.Load()

	require.NoError(t, err)
	assert.Contains(t, got.Protected, "~/keep")
	assert.Subset(t, got.Protected, DefaultProtected())
}

func TestConfigValidate_ProtectedPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Protected = append(cfg.Protected, "relative/dir")
	require.Error(t, cfg.Validate())

	cfg.Protected = MergeProtected([]string{"~/ok", "/abs/ok"})
	require.NoError(t, cfg.Validate())
}

func TestLoader_RejectsUnsafeProtectedEntries(t *testing.T) {
	for _, entry := range []string{"Downloads2", "$HOME/x", "~", "~/", "/"} {
		t.Run(entry, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("version: \"1\"\nprotected:\n  - \""+entry+"\"\n"), 0o600))
			loader := NewLoader()
			loader.SetConfigPath(path)

			_, err := loader.Load()

			require.Error(t, err)
		})
	}
}

func TestValidateProtected_RejectsBroadAndMalformedEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bad := []string{
		"~/.", "~/..", "/ ", "//", "/./", "/", "~", "~/", " /x", "/x ", "~/x/", "/x//y",
		"/x/./y", "/x/../y", "$HOME/x", "rel/dir", home, filepath.Dir(home), "/Users",
		"~/a/..", home + "/.", filepath.Dir(filepath.Dir(home)),
		"~/scratch/*", "/data/*/keep", "/data/k?ep", "/data/[ab]/keep",
		"/System/Volumes/Data", "/System/Volumes/Data/", "/System/Volumes/Data/Users",
		"/system/volumes/data" + home, "/System/Volumes/Data" + home,
	}
	for _, entry := range bad {
		t.Run(entry, func(t *testing.T) {
			cfg := &Config{Protected: []string{entry}}
			require.Error(t, cfg.validateProtected())
		})
	}
	good := &Config{Protected: []string{"~/keep", "/data/keep", filepath.Join(home, "x", "y")}}
	require.NoError(t, good.validateProtected())
}

func TestValidateProtected_RejectsSymlinkToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	link := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(home, link))

	cfg := &Config{Protected: []string{link}}

	require.Error(t, cfg.validateProtected())
}

func TestLoader_SaveWritesOnlyUserProtectedEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "config.yaml")
	loader := NewLoader()
	loader.SetConfigPath(path)
	cfg := DefaultConfig()
	cfg.Protected = MergeProtected([]string{"~/keep"})
	require.NoError(t, loader.Save(cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "~/keep")
	assert.NotContains(t, string(raw), "Downloads")
	assert.NotContains(t, string(raw), "opencode")

	reloader := NewLoader()
	reloader.SetConfigPath(path)
	got, err := reloader.Load()
	require.NoError(t, err)
	assert.Equal(t, MergeProtected([]string{"~/keep"}), got.Protected)

	require.NoError(t, reloader.Save(DefaultConfig()))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "~/keep")
}
