package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bigFixtureFiles = 50000
	conciseMaxLines = 60
)

// bigCacheFixture builds a cache with many stale files; file i has size
// i%7+1 bytes so the largest entries are well defined.
func bigCacheFixture(t *testing.T, files int) (cacheDir string, loader *config.Loader) {
	t.Helper()
	return bigCacheFixtureWith(t, files, "")
}

func bigCacheFixtureWith(t *testing.T, files int, extraProviders string) (cacheDir string, loader *config.Loader) {
	t.Helper()
	root := t.TempDir()
	cacheDir = filepath.Join(root, "cache")
	for i := range files {
		dir := filepath.Join(cacheDir, fmt.Sprintf("d%03d", i%100))
		if i < 100 {
			require.NoError(t, os.MkdirAll(dir, 0o750))
		}
		path := filepath.Join(dir, fmt.Sprintf("f%05d", i))
		require.NoError(t, os.WriteFile(path, make([]byte, i%7+1), 0o600))
		require.NoError(t, os.Chtimes(path, oldTime(), oldTime()))
	}
	cfgPath := filepath.Join(root, "config.yaml")
	cfg := `version: "1"
providers:
  big:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1G
    max_age: 1d
    clean_cmd: "true"
` + extraProviders
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	loader = config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	return cacheDir, loader
}

func lineCount(s string) int {
	return strings.Count(s, "\n")
}

func TestDryRun_ConciseByDefaultVerboseListsEveryFile(t *testing.T) {
	_, loader := bigCacheFixture(t, bigFixtureFiles)

	t.Run("auto", func(t *testing.T) {
		f := newAutoFixture(t, 1*autoGiB, "")

		require.NoError(t, runAutoWithLoader(t.Context(), loader, f.env, true))
		concise := f.out.String()
		assert.Less(t, lineCount(concise), conciseMaxLines, concise)
		assert.Contains(t, concise, "big: would free")
		assert.Contains(t, concise, "50000 entries")
		assert.Contains(t, concise, "largest:")
		assert.Contains(t, concise, "total: would free")
		assert.Contains(t, concise, "and 49995 more")

		f.out.Reset()
		f.env.verbose = true
		require.NoError(t, runAutoWithLoader(t.Context(), loader, f.env, true))
		assert.GreaterOrEqual(t, lineCount(f.out.String()), bigFixtureFiles)
		assert.Contains(t, f.out.String(), "would delete: ")
	})

	t.Run("clean", func(t *testing.T) {
		var err error
		concise := captureStdout(t, func() {
			err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{dryRun: true, smart: true}, os.Stdin)
		})
		require.NoError(t, err)
		assert.Less(t, lineCount(concise), conciseMaxLines, concise)
		assert.Contains(t, concise, "big: would free")
		assert.Contains(t, concise, "total: would free")

		verbose := captureStdout(t, func() {
			err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{dryRun: true, smart: true, verbose: true}, os.Stdin)
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, lineCount(verbose), bigFixtureFiles)
	})
}

func TestCleanDryRun_JSONKeepsFieldsAndAddsSummary(t *testing.T) {
	_, loader := bigCacheFixture(t, 300)

	var err error
	out := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{dryRun: true, smart: true, json: true}, os.Stdin)
	})
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	for _, key := range []string{"total", "providers", "total_bytes", "dry_run"} {
		assert.Contains(t, raw, key)
	}
	var parsed struct {
		Summary struct {
			Action    string `json:"action"`
			Providers int    `json:"providers"`
			Entries   int    `json:"entries"`
			Bytes     int64  `json:"bytes"`
		} `json:"summary"`
		Providers []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Output  string `json:"output"`
			Summary struct {
				Action  string `json:"action"`
				Entries int    `json:"entries"`
				Top     []struct {
					Path string `json:"path"`
					Size int64  `json:"size_bytes"`
				} `json:"top"`
			} `json:"summary"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed.Providers, 1)
	p := parsed.Providers[0]
	assert.Equal(t, "dry-run", p.Status)
	assert.NotEmpty(t, p.Output, "existing output field stays")
	assert.Equal(t, "would free", p.Summary.Action)
	assert.Equal(t, 300, p.Summary.Entries)
	require.Len(t, p.Summary.Top, 5)
	assert.Equal(t, int64(7), p.Summary.Top[0].Size)
	assert.Equal(t, 1, parsed.Summary.Providers)
	assert.Equal(t, 300, parsed.Summary.Entries)
	assert.Positive(t, parsed.Summary.Bytes)
}

func TestRealClean_ShowsLargestEntriesAndVerboseKeepsOldLine(t *testing.T) {
	_, loader := bigCacheFixture(t, 40)
	var err error
	concise := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{force: true, smart: true}, os.Stdin)
	})
	require.NoError(t, err)
	assert.Contains(t, concise, "done (freed ")
	assert.Contains(t, concise, "40 entries)")
	assert.Contains(t, concise, "largest:")
	assert.Contains(t, concise, "and 35 more")

	_, loader = bigCacheFixture(t, 40)
	verbose := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{force: true, smart: true, verbose: true}, os.Stdin)
	})
	require.NoError(t, err)
	assert.Regexp(t, `done \(freed [^,)]+\)\n`, verbose)
	assert.NotContains(t, verbose, "largest:")
}

func TestCleanDryRun_ListsUnavailableProvidersAsSkipped(t *testing.T) {
	cacheDir, _ := bigCacheFixture(t, 1)
	_, loader := bigCacheFixtureWith(t, 3, "  ghost:\n    enabled: true\n    paths:\n      - "+cacheDir+"\n    max_size: 1G\n    clean_cmd: \"bilgie-no-such-tool-xyz clean\"\n")

	var err error
	out := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big", "ghost"}, cleanOptions{dryRun: true, smart: true}, os.Stdin)
	})
	require.NoError(t, err)
	assert.Contains(t, out, "skipped (1):\n  ghost: ")
	assert.Contains(t, out, "1 skipped")
}

const ghostProvider = "  ghost:\n    enabled: true\n    max_size: 1G\n    clean_cmd: \"bilgie-no-such-tool-xyz clean\"\n    paths:\n      - "

func ghostLoader(t *testing.T) *config.Loader {
	t.Helper()
	cacheDir, _ := bigCacheFixture(t, 1)
	_, loader := bigCacheFixtureWith(t, 3, ghostProvider+cacheDir+"\n")
	return loader
}

func TestCleanRealRun_ListsUnavailableProvidersAsSkipped(t *testing.T) {
	loader := ghostLoader(t)

	var err error
	out := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big", "ghost"}, cleanOptions{force: true, smart: true}, os.Stdin)
	})

	require.NoError(t, err)
	assert.Contains(t, out, "skipped (1):\n  ghost: ")
	assert.Less(t, strings.Index(out, "skipped (1):"), strings.Index(out, "Total:"))
}

func TestCleanDryRun_CancelledStillPrintsSkippedAndTotal(t *testing.T) {
	loader := ghostLoader(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var err error
	out := captureStdout(t, func() {
		err = runCleanWithContext(func() (context.Context, context.CancelFunc) { return ctx, func() {} },
			loader, []string{"big", "ghost"}, cleanOptions{dryRun: true, smart: true}, os.Stdin)
	})

	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled")
	assert.Contains(t, out, "skipped (1):\n  ghost: ")
	assert.Contains(t, out, "total: would free")
}

func TestCleanDryRun_QuietPrintsTheByteTotal(t *testing.T) {
	_, loader := bigCacheFixture(t, 5)

	var err error
	out := captureStdout(t, func() {
		err = runCleanWithOptions(loader, []string{"big"}, cleanOptions{dryRun: true, quiet: true, smart: true}, os.Stdin)
	})

	require.NoError(t, err)
	assert.Regexp(t, `^\d+(\.\d+)? [KMG]?i?B\n$`, out)
}

func TestFinishClean_CancelledDryRunIsNotAFailure(t *testing.T) {
	results := []ProviderCleanResult{{Name: "go", Status: statusError, Error: "boom"}}
	var err error
	out := captureStdout(t, func() {
		err = finishClean(results, 0, cleanOptions{dryRun: true}, []string{"go: boom"}, true)
	})
	require.NoError(t, err)
	assert.Contains(t, out, "total: would free")
}
