package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// busyUVLoader returns a config whose uv cache holds a live flock, plus an
// idle command provider, so uv is deterministically busy.
func busyUVLoader(t *testing.T, extra string) (loader *config.Loader, lockPath string) {
	t.Helper()

	uvDir := t.TempDir()
	lockPath = filepath.Join(uvDir, ".lock")
	lock, err := os.Create(lockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX))

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := `version: "1"
providers:
  uv:
    enabled: true
    paths:
      - ` + uvDir + `
    max_size: 1
  idle:
    enabled: true
    paths:
      - ` + t.TempDir() + `
    max_size: 1GB
    clean_cmd: "echo cleaned"
` + extra
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	loader = config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	return loader, lockPath
}

func TestClean_BusyProviderSkippedInText(t *testing.T) {
	loader, lockPath := busyUVLoader(t, "")

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, nil, true, false, true, false, false, os.Stdin)
	})

	require.NoError(t, err, "a skip is not a failure")
	assert.Contains(t, output, "Cleaning uv... skipped (lock held: "+lockPath+")")
	assert.Contains(t, output, "Cleaning idle... done")
}

func TestClean_BusyProviderSkippedInJSON(t *testing.T) {
	loader, lockPath := busyUVLoader(t, "")

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, nil, cleanOptions{all: true, force: true, json: true}, os.Stdin)
	})
	require.NoError(t, err)

	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)

	byName := map[string]ProviderCleanResult{}
	for _, r := range got.Providers {
		byName[r.Name] = r
	}
	assert.Equal(t, statusSkipped, byName["uv"].Status)
	assert.Equal(t, "lock held: "+lockPath, byName["uv"].Reason)
	assert.Equal(t, statusCleaned, byName["idle"].Status)
}

func TestClean_HungCommandReportedInJSON(t *testing.T) {
	loader, _ := busyUVLoader(t, `  hung:
    enabled: true
    paths:
      - `+t.TempDir()+`
    max_size: 1GB
    clean_cmd: 'sh -c "sleep 30"'
    clean_timeout: 1s
`)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"hung"}, cleanOptions{force: true, json: true}, os.Stdin)
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after 1s")

	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)
	require.Len(t, got.Providers, 1)
	assert.Equal(t, statusError, got.Providers[0].Status)
	assert.Contains(t, got.Providers[0].Error, "timed out after 1s")
}

func TestClean_JSONRequiresForceOrDryRun(t *testing.T) {
	loader, _ := busyUVLoader(t, "")

	err := runCleanWithOptions(loader, nil, cleanOptions{all: true, json: true}, os.Stdin)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--json requires --force or --dry-run")
}

func TestClean_JSONDryRunReportsSkip(t *testing.T) {
	loader, _ := busyUVLoader(t, "")

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, nil, cleanOptions{all: true, dryRun: true, json: true}, os.Stdin)
	})
	require.NoError(t, err)

	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)
	assert.True(t, got.DryRun)
	statuses := map[string]string{}
	for _, r := range got.Providers {
		statuses[r.Name] = r.Status
	}
	assert.Equal(t, statusSkipped, statuses["uv"])
	assert.Equal(t, statusDryRun, statuses["idle"])
}

func TestCleanCmd_HasJSONFlag(t *testing.T) {
	assert.NotNil(t, CleanCmd.Flags().Lookup("json"))
}

func TestClean_JSONWithNoAvailableProviders(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := `version: "1"
providers:
  ghost:
    enabled: true
    paths:
      - ` + t.TempDir() + `
    max_size: 1GB
    clean_cmd: "definitely-not-a-real-binary-xyz clean"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, nil, cleanOptions{all: true, force: true, json: true}, os.Stdin)
	})

	require.Error(t, err)
	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)
	require.Len(t, got.Providers, 1)
	assert.Equal(t, statusUnavailable, got.Providers[0].Status)
}

func TestClean_InterruptedCleanIsNotAnErrorInJSON(t *testing.T) {
	loader, _ := busyUVLoader(t, `  slow:
    enabled: true
    paths:
      - `+t.TempDir()+`
    max_size: 1GB
    clean_cmd: 'sh -c "sleep 30"'
    clean_timeout: 100s
`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithContext(func() (context.Context, context.CancelFunc) { return ctx, cancel }, loader, []string{"slow"}, cleanOptions{force: true, json: true}, os.Stdin)
	})

	require.NoError(t, err)
	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)
	assert.True(t, got.Cancelled)
}
