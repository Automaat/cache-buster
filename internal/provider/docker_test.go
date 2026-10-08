package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/kballard/go-shellquote"
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
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestDockerDataSize_SumsRows(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo '{"Size":"1.5GB"}'
echo '{"Size":"500MB"}'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	total, err := p.dockerDataSize(t.Context())
	require.NoError(t, err)
	// 1.5GB = 1.5 * 1024^3, 500MB = 500 * 1024^2
	assert.Equal(t, int64(1.5*1024*1024*1024)+int64(500*1024*1024), total)
}

func TestDockerDataSize_IncludesVolumesAndBuildCacheRows(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo '{"Type":"Local Volumes","TotalCount":"2","Size":"2GB"}'
echo '{"Type":"Build Cache","TotalCount":"8","Size":"750MB"}'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	total, err := p.dockerDataSize(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(2*1024*1024*1024)+int64(750*1024*1024), total)
}

func TestDockerDataSize_SkipsInvalidLines(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo 'not json'
echo '{"Size":"1GB"}'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	total, err := p.dockerDataSize(t.Context())
	require.NoError(t, err)
	// 1GB = 1 * 1024^3 (size package treats GB as binary)
	assert.Equal(t, int64(1024*1024*1024), total)
}

func TestDockerDataSize_AllInvalidLines_ReturnsError(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo 'not json'
echo 'also not json'
`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize(t.Context())
	require.Error(t, err)
}

func TestDockerDataSize_EmptyOutput_ReturnsError(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `exit 0`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parsable output")
}

func TestDockerDataSize_CommandFails_IncludesStderr(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo "daemon not running" >&2; exit 1`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	_, err := p.dockerDataSize(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon not running")
}

func TestDockerCurrentSize_FallsBackToPathBased(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	// fake docker that exits non-zero
	fakeDockerBin(t, `exit 1`)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.bin"), []byte("hello"), 0o600))

	p := newTestDockerProvider(t, []string{tmpDir})
	size, err := p.CurrentSize(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(5), size)
}

func TestDockerSmartCleanDryRun_NeverIncludesVolumes(t *testing.T) {
	fakeDockerBin(t, `exit 0`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	result, err := p.Clean(t.Context(), CleanOptions{
		Mode:   CleanModeSmart,
		DryRun: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "would run: docker system prune -af --filter until=720h", result.Output)
}

func TestDockerSmartClean_DaemonUnavailable(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	// docker ps fails => daemon down => Clean returns without pruning.
	fakeDockerBin(t, `case "$1 $2" in
"ps --quiet") exit 1 ;;
esac
exit 1`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)
	assert.Equal(t, "docker not available", result.Output)
}

func TestDockerSmartClean_NothingToPrune(t *testing.T) {
	// Size unchanged before/after prune => zero bytes cleaned.
	fakeDockerBin(t, `case "$1 $2" in
"ps --quiet") exit 0 ;;
"system df") echo '{"Size":"2GB"}'; exit 0 ;;
"system prune") echo "Total reclaimed space: 0B"; exit 0 ;;
esac`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.BytesCleaned)
	assert.Contains(t, result.Output, "Total reclaimed space: 0B")
}

func TestDockerSmartClean_FreesSpace(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	// docker system df reports 5GB before the prune and 1GB after it,
	// keyed off a marker file the fake prune drops.
	marker := filepath.Join(t.TempDir(), "pruned")
	fakeDockerBin(t, `case "$1 $2" in
"ps --quiet") exit 0 ;;
"system df")
  if [ -f "`+marker+`" ]; then echo '{"Size":"1GB"}'; else echo '{"Size":"5GB"}'; fi
  exit 0 ;;
"system prune")
  touch "`+marker+`"
  echo "Total reclaimed space: 4GB"
  exit 0 ;;
esac`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)
	assert.Equal(t, int64(4*1024*1024*1024), result.BytesCleaned)
	assert.Contains(t, result.Output, "Total reclaimed space: 4GB")
}

func TestDockerSmartClean_PruneFails(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `case "$1 $2" in
"ps --quiet") exit 0 ;;
"system df") echo '{"Size":"2GB"}'; exit 0 ;;
"system prune") echo "Error response from daemon: prune failed" >&2; exit 1 ;;
esac`)

	p := newTestDockerProvider(t, []string{t.TempDir()})
	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})
	require.Error(t, err)
	assert.Contains(t, result.Output, "prune failed")
}

func TestDockerFullCleanDryRun_DefaultNeverIncludesVolumes(t *testing.T) {
	fakeDockerBin(t, `exit 0`)

	cfg, ok := config.DefaultConfig().GetProvider("docker")
	require.True(t, ok)
	cfg.Paths = []string{t.TempDir()}
	p, err := NewProvider("docker", cfg)
	require.NoError(t, err)

	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeFull, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, "would run: docker system prune -af", result.Output)
	assert.NotContains(t, result.Output, "--volumes")
}

func TestDockerVolumesDefault_DisabledAndPrunesVolumes(t *testing.T) {
	fakeDockerBin(t, `exit 0`)

	cfg, ok := config.DefaultConfig().GetProvider("docker-volumes")
	require.True(t, ok)
	assert.False(t, cfg.Enabled)
	cfg.Paths = []string{t.TempDir()}

	p, err := NewProvider("docker-volumes", cfg)
	require.NoError(t, err)

	for _, mode := range []CleanMode{CleanModeFull, CleanModeSmart} {
		result, cleanErr := p.Clean(t.Context(), CleanOptions{Mode: mode, DryRun: true})
		require.NoError(t, cleanErr)
		assert.Equal(t, "would run: docker volume prune -f", result.Output)
	}
}

func TestDockerVolumesSize_OnlyCountsVolumesRow(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo '{"Type":"Images","Size":"9GB"}'
echo '{"Type":"Local Volumes","Size":"2GB"}'
echo '{"Type":"Build Cache","Size":"750MB"}'
`)

	p, err := NewDockerVolumesProvider("docker-volumes", config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "10G",
		CleanCmd: "docker volume prune -f",
	})
	require.NoError(t, err)

	total, err := p.CurrentSize(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(2*1024*1024*1024), total)
}

func TestDockerLegacyConfigVolumesFlagStripped(t *testing.T) {
	fakeDockerBin(t, `exit 0`)

	p, err := NewProvider("docker", config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "10G",
		CleanCmd: "docker system prune -af --volumes",
	})
	require.NoError(t, err)

	result, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeFull, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, "would run: docker system prune -af", result.Output)
}

func TestDockerVolumesSize_NoVolumesRow_NoPathFallback(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, `echo '{"Type":"Images","Size":"9GB"}'
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Docker.raw"), []byte("hello"), 0o600))

	p, err := NewDockerVolumesProvider("docker-volumes", config.Provider{
		Paths:    []string{dir},
		MaxSize:  "10G",
		CleanCmd: "docker volume prune -f",
	})
	require.NoError(t, err)

	_, err = p.CurrentSize(t.Context())
	require.Error(t, err)
}

func TestStripVolumesFlag(t *testing.T) {
	tests := map[string]string{
		"docker system prune -af --volumes":          "docker system prune -af",
		"docker system prune --volumes=true -af":     "docker system prune -af",
		`sh -c "docker system prune -af --volumes"`:  "sh -c 'docker system prune -af'",
		"sh -c 'docker system prune -af\t--volumes'": "sh -c 'docker system prune -af'",
		"docker system prune -af --volumes false":    "docker system prune -af",
		"docker system prune -af":                    "docker system prune -af",
		"docker volume prune -f":                     "docker volume prune -f",
	}
	for in, want := range tests {
		assert.Equal(t, want, stripVolumesFlag(in), in)
	}
}

func TestStripVolumesFlag_Wrapped(t *testing.T) {
	tests := map[string]string{
		`sh -c "docker system prune -af '--volumes'"`:          "docker system prune -af",
		`sh -c "docker system prune -af \"--volumes\""`:        "docker system prune -af",
		`sh -c "docker system prune --volumes=true;echo ok"`:   "docker system prune;echo ok",
		`sh -c "docker system prune --volumes false"`:          "docker system prune",
		`sh -c "docker system prune --volumes falsey"`:         "docker system prune falsey",
		`sh -c "docker system prune --volumes=x&&echo ok"`:     "docker system prune&&echo ok",
		`sh -c "docker system prune --volumes=true)"`:          "docker system prune)",
		`sh -c "docker system prune --volumes=true>/dev/null"`: "docker system prune>/dev/null",
		`sh -c "docker system prune -af --volumes=\"true\""`:   "docker system prune -af",
		`sh -c "docker system prune -af --volumes='a b' -f"`:   "docker system prune -af -f",
		`sh -c "docker system prune '--volumes=true' -f"`:      "docker system prune -f",
		`sh -c "docker system prune --volumes-from x"`:         "docker system prune --volumes-from x",
		`sh -c "docker system prune --volumes --volumes"`:      "docker system prune",
		`sh -c "docker system prune --volumes"`:                "docker system prune",
	}
	for in, want := range tests {
		parts, err := shellquote.Split(stripVolumesFlag(in))
		require.NoError(t, err, in)
		require.Len(t, parts, 3, in)
		assert.Equal(t, want, parts[2], in)
	}
}

const hangingPruneDocker = `case "$1" in
ps) exit 0 ;;
system)
  if [ "$2" = df ]; then echo '{"Size":"1GB"}'; exit 0; fi
  sleep 30 ;;
esac
`

func TestDockerClean_TimeoutBoundsHungPrune(t *testing.T) {
	skipOnWindows(t, "#206 docker fakes are POSIX shell scripts")
	fakeDockerBin(t, hangingPruneDocker)
	p, err := NewDockerProvider("docker", config.Provider{
		Paths:    []string{t.TempDir()},
		MaxSize:  "10G",
		CleanCmd: "docker system prune -af",
	})
	require.NoError(t, err)

	for _, mode := range []CleanMode{CleanModeSmart, CleanModeFull} {
		start := time.Now()
		_, err = p.Clean(t.Context(), CleanOptions{Mode: mode, Timeout: 300 * time.Millisecond})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out after 300ms")
		assert.Less(t, time.Since(start), 10*time.Second)
	}
}

func TestDockerClean_ZeroTimeoutStaysUnbounded(t *testing.T) {
	fakeDockerBin(t, `case "$1" in
ps) exit 0 ;;
system)
  if [ "$2" = df ]; then echo '{"Size":"1GB"}'; exit 0; fi
  sleep 1 ;;
esac
`)
	p := newTestDockerProvider(t, []string{t.TempDir()})

	_, err := p.Clean(t.Context(), CleanOptions{Mode: CleanModeSmart})

	require.NoError(t, err)
}
