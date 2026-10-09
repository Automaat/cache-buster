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
