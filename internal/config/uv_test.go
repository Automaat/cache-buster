package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderPathsExist_UVFollowsCacheEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	moved := filepath.Join(t.TempDir(), "moved")
	t.Setenv("UV_CACHE_DIR", "")
	uv := DefaultProviders()["uv"]

	assert.False(t, ProviderPathsExist("uv", uv), "no default dir and no env")

	require.NoError(t, os.MkdirAll(moved, 0o700))
	t.Setenv("UV_CACHE_DIR", moved)
	assert.True(t, ProviderPathsExist("uv", uv), "UV_CACHE_DIR points at an existing dir")

	explicit := Provider{Paths: []string{filepath.Join(t.TempDir(), "absent")}}
	assert.False(t, ProviderPathsExist("uv", explicit), "an explicit path ignores the environment")
	assert.False(t, ProviderPathsExist("other", uv), "only uv resolves through the environment")
}

func TestProviderPathsExist_UVEnvDirIsLiteral(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	literal := filepath.Join(filepath.Clean(t.TempDir()), "cache[1]", "uv")
	require.NoError(t, os.MkdirAll(literal, 0o700))
	t.Setenv("UV_CACHE_DIR", literal)

	assert.True(t, ProviderPathsExist("uv", DefaultProviders()["uv"]))

	cfg := &Config{Providers: map[string]Provider{"uv": DefaultProviders()["uv"]}}
	assert.Equal(t, []string{"uv"}, cfg.EnabledProviders())
}

func TestProviderPathsExist_UVBadPathsStayVisible(t *testing.T) {
	assert.True(t, ProviderPathsExist("uv", Provider{Paths: []string{}}), "a load error must name the provider")
	assert.True(t, ProviderPathsExist("uv", Provider{Paths: []string{""}}))
}
