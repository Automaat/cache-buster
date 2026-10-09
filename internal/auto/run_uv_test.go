package auto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasTargets_UVFollowsCacheEnvLiterally(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	literal := filepath.Join(filepath.Clean(t.TempDir()), "cache[1]", "uv")
	t.Setenv("UV_CACHE_DIR", literal)
	pc := config.DefaultProviders()["uv"]

	assert.False(t, hasTargets("uv", pc), "the moved cache does not exist yet")

	require.NoError(t, os.MkdirAll(literal, 0o700))
	assert.True(t, hasTargets("uv", pc))
}

func TestCoveredPaths_UVUsesTheResolvedLiteralDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	moved := filepath.Join(filepath.Clean(t.TempDir()), "cache[1]", "uv")
	t.Setenv("UV_CACHE_DIR", moved)
	cfg := &config.Config{Providers: map[string]config.Provider{"uv": config.DefaultProviders()["uv"]}}

	assert.Equal(t, []string{moved}, CoveredPaths(cfg))
}
