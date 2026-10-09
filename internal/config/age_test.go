package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadAgeConfig(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: \"1\"\nproviders:\n"+body), 0o600))
	l := NewLoader()
	l.SetConfigPath(path)
	return l.Load()
}

func TestLoad_RejectsSecondScaleAges(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"max_age ms", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 30ms\n",
			`provider "x": max_age 30ms is under the 1m minimum; did you mean 30m?`},
		{"max_age s", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 30s\n",
			`provider "x": max_age 30s is under the 1m minimum; did you mean 30d?`},
		{"max_age bare number", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 30\n",
			`max_age 30 is under the 1m minimum; did you mean 30d?`},
		{"max_age 59s", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 59s\n",
			`max_age 59s is under the 1m minimum`},
		{"dir-pattern min_idle", "  x:\n    type: dir-pattern\n    paths: [\"/a/b*\"]\n    max_size: 1G\n    min_idle: 10s\n",
			`provider "x": min_idle 10s is under the 1m minimum; did you mean 10d?`},
		{"project-artifacts min_idle", "  x:\n    type: project-artifacts\n    paths: [/a]\n    max_size: 1G\n    min_idle: 10s\n",
			`provider "x": min_idle 10s is under the 1m minimum`},
		{"overrides a built-in", "  npm:\n    max_age: 30s\n",
			`provider "npm": max_age 30s is under the 1m minimum`},
		{"disabled provider", "  x:\n    enabled: false\n    paths: [/a]\n    max_size: 1G\n    max_age: 30s\n",
			`max_age 30s is under the 1m minimum`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadAgeConfig(t, tt.body)
			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), tt.want)
			var floor *AgeFloorError
			assert.ErrorAs(t, err, &floor)
		})
	}
}

func TestLoad_AcceptsLegitimateDurations(t *testing.T) {
	tests := []struct{ name, body string }{
		{"30m", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 30m\n"},
		{"30d", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 30d\n"},
		{"1h", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 1h\n"},
		{"60s", "  x:\n    paths: [/a]\n    max_size: 1G\n    max_age: 60s\n"},
		{"scan budget ms", "  x:\n    type: project-artifacts\n    paths: [/a]\n    max_size: 1G\n    scan_budget: 500ms\n    pass_budget: 750ms\n"},
		{"clean timeout ms", "  x:\n    paths: [/a]\n    max_size: 1G\n    clean_cmd: \"true\"\n    clean_timeout: 500ms\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadAgeConfig(t, tt.body)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate())
		})
	}
}

func TestValidate_RejectsMillisecondAge(t *testing.T) {
	cfg := &Config{Auto: DefaultAuto(), Providers: map[string]Provider{
		"x": {Paths: []string{"/a"}, MaxSize: "1G", MinIdle: "10ms"},
	}}
	require.Error(t, cfg.Validate())
}

func TestDefaultsPassAgeFloor(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		cfg := DefaultConfigFor(Platform{OS: goos})
		require.NoError(t, cfg.ValidateAges(), goos)
	}
}

func TestAgeFloorError_NoSuggestionForZero(t *testing.T) {
	for _, v := range []string{"0", "0s", "0ms"} {
		err := &AgeFloorError{Provider: "x", Field: "max_age", Value: v}
		assert.NotContains(t, err.Error(), "did you mean", v)
	}
}

func TestAuto_SpansKeepMillisecondUnit(t *testing.T) {
	a := DefaultAuto()
	a.NotifyCooldown = "250ms"
	_, err := a.Limits()
	require.NoError(t, err)
}
