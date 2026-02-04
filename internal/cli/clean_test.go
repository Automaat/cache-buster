package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTempConfig(t *testing.T, cacheDir string) *config.Loader {
	t.Helper()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `version: "1"
providers:
  test-provider:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1GB
    clean_cmd: "echo cleaned"
  disabled-provider:
    enabled: false
    paths:
      - /tmp
    max_size: 1GB
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)
	return loader
}

func createStdinWithInput(t *testing.T, input string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(input)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestClean_NoArgsNoAll(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	err := runCleanWithLoader(loader, nil, false, false, false, false, false, os.Stdin)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify providers or use --all")
	assert.Contains(t, err.Error(), "test-provider")
}

func TestClean_UnknownProvider(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	err := runCleanWithLoader(loader, []string{"nonexistent"}, false, false, false, false, false, os.Stdin)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown providers: nonexistent")
	assert.Contains(t, err.Error(), "Available: test-provider")
}

func TestClean_DryRun_All(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, nil, true, true, false, false, false, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "[dry-run]")
	assert.Contains(t, output, "test-provider")
	assert.Contains(t, output, "would run")
}

func TestClean_DryRun_SpecificProvider(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, true, false, false, false, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "[dry-run]")
	assert.Contains(t, output, "test-provider")
}

func TestClean_Force_SkipsConfirmation(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, false, true, false, false, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "Cleaning test-provider")
	assert.Contains(t, output, "done")
	assert.Contains(t, output, "Total:")
}

func TestClean_ConfirmationYes(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)
	stdin := createStdinWithInput(t, "y\n")

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, false, false, false, false, stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "Cleaning test-provider")
	assert.Contains(t, output, "done")
}

func TestClean_ConfirmationNo(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)
	stdin := createStdinWithInput(t, "n\n")

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, false, false, false, false, stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "Aborted")
	assert.NotContains(t, output, "Cleaning")
}

func TestClean_QuietMode(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, false, true, true, false, os.Stdin)
	})
	require.NoError(t, err)

	assert.NotContains(t, output, "Cleaning")
	assert.NotContains(t, output, "Total:")
	output = strings.TrimSpace(output)
	assert.NotEmpty(t, output)
}

func TestClean_NoConfig_CreatesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nonexistent.yaml")
	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)

	// dry-run with all providers - should succeed with default config
	err := runCleanWithLoader(loader, nil, true, true, false, true, false, os.Stdin)

	require.NoError(t, err)
	// verify config was created
	_, statErr := os.Stat(cfgPath)
	assert.NoError(t, statErr, "config file should be created")
}

func TestCleanCmd_HasFlags(t *testing.T) {
	tests := []struct {
		name     string
		defValue string
	}{
		{"all", "false"},
		{"dry-run", "false"},
		{"force", "false"},
		{"quiet", "false"},
		{"smart", "false"},
	}

	for _, tt := range tests {
		flag := CleanCmd.Flags().Lookup(tt.name)
		require.NotNil(t, flag, "flag %s should exist", tt.name)
		assert.Equal(t, tt.defValue, flag.DefValue, "flag %s default", tt.name)
	}
}

func TestResolveProviders_All(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"prov1": {Enabled: true, Paths: []string{"/tmp"}, MaxSize: "1GB"},
			"prov2": {Enabled: true, Paths: []string{"/tmp"}, MaxSize: "1GB"},
			"prov3": {Enabled: false, Paths: []string{"/tmp"}, MaxSize: "1GB"},
		},
	}

	names, err := resolveProviders(cfg, nil, true)
	require.NoError(t, err)

	assert.Len(t, names, 2)
	assert.Contains(t, names, "prov1")
	assert.Contains(t, names, "prov2")
	assert.NotContains(t, names, "prov3")
}

func TestResolveProviders_Specific(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"prov1": {Enabled: true, Paths: []string{"/tmp"}, MaxSize: "1GB"},
			"prov2": {Enabled: true, Paths: []string{"/tmp"}, MaxSize: "1GB"},
		},
	}

	names, err := resolveProviders(cfg, []string{"prov1"}, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"prov1"}, names)
}

func TestResolveProviders_InvalidProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{
			"prov1": {Enabled: true, Paths: []string{"/tmp"}, MaxSize: "1GB"},
		},
	}

	_, err := resolveProviders(cfg, []string{"invalid"}, false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown providers: invalid")
}

func TestConfirmClean_Yes(t *testing.T) {
	stdin := createStdinWithInput(t, "y\n")
	var result bool
	captureStdout(t, func() {
		result = confirmClean(nil, false, stdin)
	})
	assert.True(t, result)
}

func TestConfirmClean_No(t *testing.T) {
	stdin := createStdinWithInput(t, "n\n")
	var result bool
	captureStdout(t, func() {
		result = confirmClean(nil, false, stdin)
	})
	assert.False(t, result)
}

func TestConfirmClean_Empty(t *testing.T) {
	stdin := createStdinWithInput(t, "\n")
	var result bool
	captureStdout(t, func() {
		result = confirmClean(nil, false, stdin)
	})
	assert.False(t, result)
}

func TestClean_AllWithForce(t *testing.T) {
	cacheDir := t.TempDir()
	loader := createTempConfig(t, cacheDir)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, nil, true, false, true, false, false, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "Cleaning test-provider")
	assert.Contains(t, output, "done")
	assert.Contains(t, output, "Total:")
}

func TestClean_ProviderFailure(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cacheDir := t.TempDir()
	cfgContent := `version: "1"
providers:
  failing-provider:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1GB
    clean_cmd: "/nonexistent-command-that-does-not-exist"
  working-provider:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1GB
    clean_cmd: "echo cleaned"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, nil, true, false, true, false, false, os.Stdin)
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some providers failed")
	assert.Contains(t, err.Error(), "failing-provider")
	assert.Contains(t, output, "working-provider")
}

func TestClean_SmartMode_DryRun(t *testing.T) {
	cacheDir := t.TempDir()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `version: "1"
providers:
  test-provider:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1GB
    max_age: 30d
    clean_cmd: "echo cleaned"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	// Create a test file
	testFile := filepath.Join(cacheDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0o600))

	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, true, false, false, true, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "[dry-run]")
	assert.Contains(t, output, "test-provider")
}

func TestClean_SmartMode_Force(t *testing.T) {
	cacheDir := t.TempDir()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `version: "1"
providers:
  test-provider:
    enabled: true
    paths:
      - ` + cacheDir + `
    max_size: 1GB
    max_age: 30d
    clean_cmd: "echo cleaned"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	loader := config.NewLoader()
	loader.SetConfigPath(cfgPath)

	var err error
	output := captureStdout(t, func() {
		err = runCleanWithLoader(loader, []string{"test-provider"}, false, false, true, false, true, os.Stdin)
	})
	require.NoError(t, err)

	assert.Contains(t, output, "Cleaning test-provider")
	assert.Contains(t, output, "done")
}
