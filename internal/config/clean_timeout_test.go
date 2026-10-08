package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_CleanTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr string
	}{
		{"unset", "", ""},
		{"valid", "90s", ""},
		{"zero", "0", "must be positive"},
		{"blank", " ", "must not be blank"},
		{"garbage", "soon", "clean_timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Providers: map[string]Provider{
				"x": {Paths: []string{"/p"}, MaxSize: "1G", CleanCmd: "x", CleanTimeout: tt.timeout},
			}}
			err := cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_CleanTimeoutRequiresCommand(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"x": {Paths: []string{"/p"}, MaxSize: "1G", CleanTimeout: "30s"},
	}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires clean_cmd")
}
