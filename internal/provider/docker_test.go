package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDockerProvider(t *testing.T, paths []string) *DockerProvider {
	t.Helper()
	p, err := NewDockerProvider("docker", config.Provider{
		Paths:    paths,
		MaxSize:  "10G",
		CleanCmd: "echo clean",
	})
	require.NoError(t, err)
	return p
}

func fakeDockerBin(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755)) //nolint:gosec // G306: test binary must be executable
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestDockerDataSize_SumsRows(t *testing.T) {
	fakeDockerBin(t, `echo '{"Size":"1.5GB"}'
echo '{"Size":"500MB"}'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	total, err := p.dockerDataSize()
	require.NoError(t, err)
	// 1.5GB = 1.5 * 1024^3, 500MB = 500 * 1024^2
	assert.Equal(t, int64(1.5*1024*1024*1024)+int64(500*1024*1024), total)
}

func TestDockerDataSize_SkipsInvalidLines(t *testing.T) {
	fakeDockerBin(t, `echo 'not json'
echo '{"Size":"1GB"}'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	total, err := p.dockerDataSize()
	require.NoError(t, err)
	// 1GB = 1 * 1024^3 (size package treats GB as binary)
	assert.Equal(t, int64(1024*1024*1024), total)
}

func TestDockerDataSize_AllInvalidLines_ReturnsError(t *testing.T) {
	fakeDockerBin(t, `echo 'not json'
echo 'also not json'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize()
	require.Error(t, err)
}

func TestDockerDataSize_EmptyOutput_ReturnsError(t *testing.T) {
	fakeDockerBin(t, `exit 0`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parsable output")
}

func TestDockerDataSize_CommandFails_IncludesStderr(t *testing.T) {
	fakeDockerBin(t, `echo "daemon not running" >&2; exit 1`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon not running")
}

func TestDockerCurrentSize_FallsBackToPathBased(t *testing.T) {
	// fake docker that exits non-zero
	fakeDockerBin(t, `exit 1`)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.bin"), []byte("hello"), 0o600))

	p := newTestDockerProvider(t, []string{tmpDir})
	size, err := p.CurrentSize()
	require.NoError(t, err)
	assert.Equal(t, int64(5), size)
}
