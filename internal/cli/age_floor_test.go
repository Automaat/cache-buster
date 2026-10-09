package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ageLoader(t *testing.T, providerBody string) *config.Loader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := "version: \"1\"\nproviders:\n" + providerBody
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
	l := config.NewLoader()
	l.SetConfigPath(path)
	l.SkipDefaults()
	return l
}

func badAgeBody(cacheDir, field, value string) string {
	return "  tool:\n    enabled: true\n    paths: [" + cacheDir + "]\n    max_size: 1G\n    clean_cmd: \"true\"\n    " +
		field + ": " + value + "\n"
}

func requireAgeError(t *testing.T, err error, field, value string) {
	t.Helper()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `provider "tool"`)
	assert.Contains(t, err.Error(), field+" "+value+" is under the 1m minimum")
}

func TestAgeFloor_RejectedOnEveryCommandPath(t *testing.T) {
	cases := []struct{ field, value string }{
		{"max_age", "30ms"}, {"max_age", "30s"}, {"min_idle", "10s"},
	}
	for _, c := range cases {
		t.Run(c.field+"="+c.value, func(t *testing.T) {
			cacheDir := t.TempDir()
			sentinel := filepath.Join(cacheDir, "old.bin")
			require.NoError(t, os.WriteFile(sentinel, []byte("x"), 0o600))
			require.NoError(t, os.Chtimes(sentinel, oldTime(), oldTime()))
			body := badAgeBody(cacheDir, c.field, c.value)

			requireAgeError(t, runConfigShowWithLoader(ageLoader(t, body)), c.field, c.value)
			requireAgeError(t, runStatusWithLoader(ageLoader(t, body), true, nil, nil), c.field, c.value)
			requireAgeError(t, runCleanWithLoader(ageLoader(t, body), []string{"tool"}, false, false, true, true, true, nil), c.field, c.value)

			f := newAutoFixture(t, 1*autoGiB, "")
			f.loader = ageLoader(t, body)
			require.Error(t, runDoctorWithLoader(t.Context(), f.loader, f.env))
			assert.Contains(t, f.out.String(), c.field+" "+c.value+" is under the 1m minimum")
			f.out.Reset()
			requireAgeError(t, runAutoWithLoader(t.Context(), f.loader, f.env, false), c.field, c.value)
			requireAgeError(t, runTickWithLoader(t.Context(), f.loader, f.env, false), c.field, c.value)

			_, err := os.Stat(sentinel)
			require.NoError(t, err, "a rejected config must delete nothing")
		})
	}
}

func TestAgeFloor_BadProviderBlocksAutoForTheWholeConfig(t *testing.T) {
	f := newAutoFixture(t, 1*autoGiB, "  other:\n    enabled: true\n    paths: [/x]\n    max_size: 1G\n    max_age: 30s\n    clean_cmd: \"true\"\n")
	err := runAutoWithLoader(t.Context(), f.loader, f.env, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `provider "other": max_age 30s`)
	assert.False(t, f.cleaned(), "auto must not run the valid provider with a broken config")
}

func TestAgeFloor_LegitimateValuesLoad(t *testing.T) {
	cacheDir := t.TempDir()
	for _, v := range []string{"30m", "30d", "1h"} {
		_, err := ageLoader(t, badAgeBody(cacheDir, "max_age", v)).Load()
		require.NoError(t, err, v)
	}
}
