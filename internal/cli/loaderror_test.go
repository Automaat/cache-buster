package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	require.NoError(t, w.Close())
	os.Stderr = old
	<-done
	return buf.String()
}

// badDirPatternLoader returns a config with one healthy provider and three
// dir-pattern providers that fail to load: a relative path, a literal path
// without a glob and an unparsable min_idle.
func badDirPatternLoader(t *testing.T) (loader *config.Loader, literal string) {
	t.Helper()
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	require.NoError(t, os.MkdirAll(cache, 0o750))
	literal = filepath.Join(root, "literal")
	require.NoError(t, os.MkdirAll(literal, 0o750))

	cfg := `version: "1"
providers:
  good:
    enabled: true
    paths:
      - ` + cache + `
    max_size: 1GB
    clean_cmd: "true"
  rel:
    enabled: true
    type: dir-pattern
    paths:
      - relative/dirs-*
    max_size: 1GB
  lit:
    enabled: true
    type: dir-pattern
    paths:
      - ` + literal + `
    max_size: 1GB
  idle:
    enabled: true
    type: dir-pattern
    min_idle: banana
    paths:
      - ` + filepath.Join(root, "none-*") + `
    max_size: 1GB
`
	cfgPath := filepath.Join(root, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	loader = config.NewLoader()
	loader.SetConfigPath(cfgPath)
	loader.SkipDefaults()
	return loader, literal
}

func TestClean_LoadErrorsNamedInTextAndOthersRun(t *testing.T) {
	loader, literal := badDirPatternLoader(t)

	var err error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			err = runCleanWithOptions(loader, nil, cleanOptions{all: true, force: true}, os.Stdin)
		})
	})

	require.NoError(t, err)
	assert.Contains(t, stderr, `provider rel: path "relative/dirs-*" must be absolute or start with ~/`)
	assert.Contains(t, stderr, `provider lit: path "`+literal+`" must contain a glob`)
	assert.Contains(t, stderr, "provider idle: parse min_idle: ")
	assert.NotContains(t, stderr, "load error")
	assert.Contains(t, stdout, "Cleaning good... done")
}

func TestClean_LoadErrorsNamedInJSON(t *testing.T) {
	loader, _ := badDirPatternLoader(t)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, nil, cleanOptions{all: true, dryRun: true, json: true}, os.Stdin)
	})
	require.NoError(t, err)

	var got CleanOutput
	require.NoError(t, json.Unmarshal([]byte(output), &got), output)
	byName := map[string]ProviderCleanResult{}
	for _, r := range got.Providers {
		byName[r.Name] = r
	}
	assert.Equal(t, statusDryRun, byName["good"].Status)
	for _, name := range []string{"rel", "lit", "idle"} {
		assert.Equal(t, statusUnavailable, byName[name].Status, name)
		assert.Contains(t, byName[name].Reason, "provider "+name+": ", name)
	}
	assert.Contains(t, byName["rel"].Reason, "must be absolute")
	assert.Contains(t, byName["lit"].Reason, "must contain a glob")
	assert.Contains(t, byName["idle"].Reason, "min_idle")
}

func TestClean_OnlyBrokenProviderNamesCause(t *testing.T) {
	loader, _ := badDirPatternLoader(t)

	err := runCleanWithOptions(loader, []string{"rel"}, cleanOptions{force: true}, os.Stdin)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `no available providers to clean: provider rel: path "relative/dirs-*" must be absolute`)
}

func TestClean_ConciseDryRunSkipListNamesCauseOnce(t *testing.T) {
	loader, _ := badDirPatternLoader(t)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithOptions(loader, nil, cleanOptions{all: true, dryRun: true}, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "\n  provider idle: parse min_idle: ")
	assert.NotContains(t, output, "idle: provider idle")
}

func TestStatus_LoadErrorsNamedInTextAndJSON(t *testing.T) {
	loader, _ := badDirPatternLoader(t)

	var err error
	text := captureStdout(t, func() {
		err = runStatusWithLoader(loader, false, nil, nil)
	})
	require.NoError(t, err)
	assert.Contains(t, text, "provider rel: path")
	assert.Contains(t, text, "provider lit: path")
	assert.Contains(t, text, "provider idle: parse min_idle")
	assert.Contains(t, text, "good")

	jsonOut := captureStdout(t, func() {
		err = runStatusWithLoader(loader, true, nil, nil)
	})
	require.NoError(t, err)
	var got StatusOutput
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &got), jsonOut)
	byName := map[string]ProviderStatus{}
	for _, s := range got.Providers {
		byName[s.Name] = s
	}
	assert.Empty(t, byName["good"].Error)
	assert.Contains(t, byName["rel"].Error, "provider rel: path")
	assert.Contains(t, byName["lit"].Error, "provider lit: path")
	assert.Contains(t, byName["idle"].Error, "provider idle: parse min_idle")
}
