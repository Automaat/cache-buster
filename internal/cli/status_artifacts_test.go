package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanProvider_ProjectArtifactsShowsRecoverable(t *testing.T) {
	root := t.TempDir()
	age := func(dir string, idle time.Duration) {
		when := time.Now().Add(-idle)
		require.NoError(t, filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, when, when)
		}))
	}
	write := func(path, data string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	}
	write(filepath.Join(root, "old", "package.json"), "{}")
	write(filepath.Join(root, "old", "node_modules", "a.js"), strings.Repeat("x", 2000))
	age(filepath.Join(root, "old"), 90*24*time.Hour)
	write(filepath.Join(root, "new", "package.json"), "{}")
	write(filepath.Join(root, "new", "node_modules", "a.js"), strings.Repeat("x", 1000))

	cfg := &config.Config{Providers: map[string]config.Provider{
		"project-artifacts": {
			Type: config.TypeProjectArtifacts, Paths: []string{root}, MaxSize: "1G", Enabled: true,
		},
	}}

	status := scanProvider(t.Context(), cfg, "project-artifacts")

	require.Empty(t, status.Error)
	assert.Equal(t, int64(3000), status.Current)
	assert.Equal(t, int64(2000), status.Recoverable)
	assert.Equal(t, "2.0 KiB", status.RecoverableFmt)
}

func TestOutputTable_ShowsRecoverable(t *testing.T) {
	out := captureStdout(t, func() {
		require.NoError(t, outputTable([]ProviderStatus{{
			Name: "project-artifacts", CurrentFmt: "3.0 KiB", Current: 3000, MaxFmt: "1.0 GiB",
			Recoverable: 2000, RecoverableFmt: "2.0 KiB",
		}}))
	})

	assert.Contains(t, out, "3.0 KiB (2.0 KiB recoverable)")
}
