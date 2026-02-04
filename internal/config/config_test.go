package config

import (
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			cfg:     DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing version",
			cfg: &Config{
				Version:   "",
				Providers: DefaultProviders(),
			},
			wantErr: true,
			errMsg:  "version is required",
		},
		{
			name: "no providers",
			cfg: &Config{
				Version:   "1",
				Providers: map[string]Provider{},
			},
			wantErr: true,
			errMsg:  "at least one provider is required",
		},
		{
			name: "provider without paths",
			cfg: &Config{
				Version: "1",
				Providers: map[string]Provider{
					"test": {Enabled: true, Paths: []string{}, MaxSize: "1G"},
				},
			},
			wantErr: true,
			errMsg:  "at least one path is required",
		},
		{
			name: "provider without max_size",
			cfg: &Config{
				Version: "1",
				Providers: map[string]Provider{
					"test": {Enabled: true, Paths: []string{"~/test"}, MaxSize: ""},
				},
			},
			wantErr: true,
			errMsg:  "max_size is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want to contain %v", err, tt.errMsg)
				}
			}
		})
	}
}

func TestConfig_GetProvider(t *testing.T) {
	cfg := DefaultConfig()

	p, ok := cfg.GetProvider("go-build")
	if !ok {
		t.Error("GetProvider() should find go-build")
	}
	if p.MaxSize != "10G" {
		t.Errorf("GetProvider() go-build MaxSize = %v, want 10G", p.MaxSize)
	}

	_, ok = cfg.GetProvider("nonexistent")
	if ok {
		t.Error("GetProvider() should not find nonexistent")
	}
}

func TestConfig_EnabledProviders(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Providers: map[string]Provider{
			"enabled1":  {Enabled: true, Paths: []string{"~/a"}, MaxSize: "1G"},
			"disabled1": {Enabled: false, Paths: []string{"~/b"}, MaxSize: "1G"},
			"enabled2":  {Enabled: true, Paths: []string{"~/c"}, MaxSize: "1G"},
		},
	}

	enabled := cfg.EnabledProviders()
	if len(enabled) != 2 {
		t.Errorf("EnabledProviders() len = %d, want 2", len(enabled))
	}

	// Should be sorted
	if enabled[0] != "enabled1" || enabled[1] != "enabled2" {
		t.Errorf("EnabledProviders() = %v, want [enabled1 enabled2]", enabled)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("DefaultConfig() version = %v, want 1", cfg.Version)
	}

	expectedProviders := []string{
		"go-build", "go-mod", "npm", "yarn", "homebrew",
		"mise", "uv", "jetbrains", "docker",
	}

	for _, name := range expectedProviders {
		if _, ok := cfg.GetProvider(name); !ok {
			t.Errorf("DefaultConfig() missing provider %v", name)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
