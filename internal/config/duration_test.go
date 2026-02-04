package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Days
		{"30d", 30 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"7D", 7 * 24 * time.Hour, false},

		// Hours
		{"24h", 24 * time.Hour, false},
		{"1H", time.Hour, false},

		// Minutes
		{"60m", 60 * time.Minute, false},
		{"30M", 30 * time.Minute, false},

		// Seconds
		{"3600s", 3600 * time.Second, false},
		{"60S", 60 * time.Second, false},
		{"100", 100 * time.Second, false}, // No unit defaults to seconds

		// Empty string returns default
		{"", DefaultMaxAge, false},
		{"  ", DefaultMaxAge, false},

		// With whitespace
		{" 30d ", 30 * 24 * time.Hour, false},
		{"30 d", 30 * 24 * time.Hour, false},

		// Errors
		{"abc", 0, true},
		{"30x", 0, true},
		{"-30d", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
