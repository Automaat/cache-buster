package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoader_DirPatternOverrides(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `version: "1"
providers:
  sail-dirs:
    enabled: true
    min_idle: 6h
    skip_if_open: false
`
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	loader := NewLoader()
	loader.SetConfigPath(configPath)
	tempDir := t.TempDir()
	loader.SetPlatform(Platform{OS: OSLinux, TempDir: tempDir})
	cfg, err := loader.Load()
	require.NoError(t, err)

	sail := cfg.Providers["sail-dirs"]
	assert.True(t, sail.Enabled)
	assert.Equal(t, TypeDirPattern, sail.Type, "type is inherited from defaults")
	assert.Equal(t, []string{filepath.Join(tempDir, "sail*")}, sail.Paths)
	assert.Equal(t, "6h", sail.MinIdle)
	require.NotNil(t, sail.SkipIfOpen)
	assert.False(t, *sail.SkipIfOpen)
	assert.Nil(t, sail.SkipIfGitWorktree, "unset guard keeps its safe default")
}

func TestLoader_DefaultSailProviderStaysDisabled(t *testing.T) {
	loader := NewLoader()
	loader.SetConfigPath(filepath.Join(t.TempDir(), "config.yaml"))

	cfg, err := loader.Load()
	require.NoError(t, err)
	assert.False(t, cfg.Providers["sail-dirs"].Enabled)
}

func TestConfig_Validate_RejectsUnknownType(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"x": {Type: "bogus", Paths: []string{"/a"}, MaxSize: "1G"},
	}}
	require.ErrorContains(t, cfg.Validate(), "unknown type")
}

func TestConfig_Validate_DirPatternRequiresGlob(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"x": {Type: TypeDirPattern, Paths: []string{"~/Documents"}, MaxSize: "1G"},
	}}
	require.ErrorContains(t, cfg.Validate(), "must contain a glob")

	cfg.Providers["x"] = Provider{Type: TypeDirPattern, Paths: []string{"~/Documents/x*"}, MaxSize: "1G"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_DirPatternRequiresAbsolute(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"x": {Type: TypeDirPattern, Paths: []string{"Documents/*"}, MaxSize: "1G"},
	}}
	require.ErrorContains(t, cfg.Validate(), "absolute")
}
