package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loaderWith(t *testing.T, yaml string) *Loader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	l := NewLoader()
	l.SetConfigPath(path)
	return l
}

func TestAuto_DefaultsWhenBlockMissing(t *testing.T) {
	cfg, err := loaderWith(t, "version: \"1\"\n").Load()
	require.NoError(t, err)

	assert.Equal(t, DefaultAuto(), cfg.Auto)
	assert.Equal(t, "30m", cfg.Auto.Interval)
	assert.Equal(t, "30G", cfg.Auto.MinFree)
	assert.InDelta(t, 5.0, cfg.Auto.MinFreePct, 0)
}

func TestAuto_PartialOverrideKeepsOtherDefaults(t *testing.T) {
	cfg, err := loaderWith(t, "auto:\n  min_free: 50G\n").Load()
	require.NoError(t, err)

	assert.Equal(t, "50G", cfg.Auto.MinFree)
	assert.Equal(t, "30m", cfg.Auto.Interval)
	assert.InDelta(t, 5.0, cfg.Auto.MinFreePct, 0)
}

func TestAuto_ExplicitZeroPercentDisablesPercentFloor(t *testing.T) {
	cfg, err := loaderWith(t, "auto:\n  min_free_pct: 0\n").Load()
	require.NoError(t, err)

	assert.InDelta(t, 0.0, cfg.Auto.MinFreePct, 0)
}

func TestAuto_Validate(t *testing.T) {
	tests := []struct {
		name    string
		auto    Auto
		wantErr string
	}{
		{"defaults", DefaultAuto(), ""},
		{"zero value uses defaults", Auto{}, ""},
		{"bad interval", Auto{Interval: "soon"}, "interval"},
		{"too short interval", Auto{Interval: "30s"}, "at least"},
		{"bad min_free", Auto{MinFree: "lots"}, "min_free"},
		{"negative pct", Auto{MinFreePct: -1}, "min_free_pct"},
		{"pct over 100", Auto{MinFreePct: 101}, "min_free_pct"},
		{"tick below a minute", Auto{TickInterval: "30s"}, "tick_interval"},
		{"tick not whole minutes", Auto{TickInterval: "90s"}, "tick_interval"},
		{"bad cap", Auto{MinFreeCap: "huge"}, "min_free_cap"},
		{"bad critical", Auto{CriticalFree: "x"}, "critical_free"},
		{"bad emergency", Auto{EmergencyFree: "x"}, "emergency_free"},
		{"bad hysteresis", Auto{Hysteresis: "x"}, "hysteresis"},
		{"bad cooldown", Auto{LowCooldown: "later"}, "low_cooldown"},
		{"bad critical cooldown", Auto{CriticalCooldown: "later"}, "critical_cooldown"},
		{"bad forecast", Auto{Forecast: "soon"}, "forecast"},
		{"bad notify cooldown", Auto{NotifyCooldown: "later"}, "notify_cooldown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&Config{Auto: tt.auto, Providers: map[string]Provider{}}).Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestAuto_IntervalDuration(t *testing.T) {
	d, err := Auto{Interval: "45m"}.IntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, d)

	d, err = Auto{}.IntervalDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, d)
}

func TestAuto_SaveKeepsDefaultsOutOfFileAndPersistsOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	l := NewLoader()
	l.SetConfigPath(path)

	require.NoError(t, l.Save(DefaultConfig()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "min_free")

	cfg := DefaultConfig()
	cfg.Auto.MinFree = "80G"
	require.NoError(t, l.Save(cfg))
	reloaded := NewLoader()
	reloaded.SetConfigPath(path)
	got, err := reloaded.Load()
	require.NoError(t, err)
	assert.Equal(t, "80G", got.Auto.MinFree)
}

func TestAuto_DefaultsMatchTheDocumentedValues(t *testing.T) {
	l, err := DefaultAuto().Limits()
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, l.TickInterval)
	assert.Equal(t, 30*time.Minute, l.Interval)
	assert.Equal(t, 10*time.Minute, l.LowCooldown)
	assert.Equal(t, 2*time.Minute, l.CriticalCooldown)
	assert.Equal(t, 15*time.Minute, l.Forecast)
	assert.Equal(t, 3*time.Hour, l.NotifyCooldown)
	assert.Equal(t, int64(30)<<30, l.MinFree)
	assert.Equal(t, int64(100)<<30, l.MinFreeCap)
	assert.Equal(t, int64(10)<<30, l.CriticalFree)
	assert.Equal(t, int64(5)<<30, l.EmergencyFree)
	assert.Equal(t, int64(2)<<30, l.Hysteresis)
	assert.InDelta(t, 5.0, l.MinFreePct, 0)
}

func TestAuto_ExplicitFifteenPercentIsKept(t *testing.T) {
	cfg, err := loaderWith(t, "auto:\n  min_free_pct: 15\n").Load()
	require.NoError(t, err)

	assert.InDelta(t, 15.0, cfg.Auto.MinFreePct, 0)
}

func TestAuto_NewFieldsOverrideOneByOne(t *testing.T) {
	cfg, err := loaderWith(t, "auto:\n  tick_interval: 5m\n  low_cooldown: 20m\n  forecast: 0\n").Load()
	require.NoError(t, err)

	assert.Equal(t, "5m", cfg.Auto.TickInterval)
	assert.Equal(t, "20m", cfg.Auto.LowCooldown)
	assert.Equal(t, "10G", cfg.Auto.CriticalFree)
	l, err := cfg.Auto.Limits()
	require.NoError(t, err)
	assert.Zero(t, l.Forecast, "0 disables the forecast")
}

func TestAuto_ZeroValueResolvesEveryField(t *testing.T) {
	assert.Equal(t, DefaultAuto().Resolved(), Auto{MinFreePct: DefaultAutoMinFreePct}.Resolved())
}
