package config

const currentVersion = "1"

// DefaultProviders returns builtin provider definitions.
func DefaultProviders() map[string]Provider {
	return map[string]Provider{
		"go-build": {
			Enabled:  true,
			Paths:    []string{"~/Library/Caches/go-build"},
			MaxSize:  "10G",
			MaxAge:   "30d",
			CleanCmd: "go clean -cache",
		},
		"go-mod": {
			Enabled:  true,
			Paths:    []string{"~/go/pkg/mod"},
			MaxSize:  "5G",
			MaxAge:   "30d",
			CleanCmd: "go clean -modcache",
		},
		"npm": {
			Enabled:  true,
			Paths:    []string{"~/.npm"},
			MaxSize:  "3G",
			MaxAge:   "30d",
			CleanCmd: "npm cache clean --force",
		},
		"yarn": {
			Enabled:  true,
			Paths:    []string{"~/Library/Caches/Yarn"},
			MaxSize:  "2G",
			MaxAge:   "30d",
			CleanCmd: "yarn cache clean",
		},
		"homebrew": {
			Enabled:  true,
			Paths:    []string{"~/Library/Caches/Homebrew"},
			MaxSize:  "5G",
			MaxAge:   "30d",
			CleanCmd: "brew cleanup",
		},
		"mise": {
			Enabled:  true,
			Paths:    []string{"~/.local/share/mise"},
			MaxSize:  "8G",
			MaxAge:   "30d",
			CleanCmd: "mise prune",
		},
		"uv": {
			Enabled:  true,
			Paths:    []string{"~/.cache/uv"},
			MaxSize:  "4G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"jetbrains": {
			Enabled:  true,
			Paths:    []string{"~/Library/Caches/JetBrains"},
			MaxSize:  "3G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"docker": {
			Enabled:  true,
			Paths:    []string{"~/Library/Containers/com.docker.docker"},
			MaxSize:  "50G",
			MaxAge:   "30d",
			CleanCmd: "docker system prune -af",
		},
	}
}

// DefaultConfig returns config with all default providers.
func DefaultConfig() *Config {
	return &Config{
		Version:   currentVersion,
		Providers: DefaultProviders(),
	}
}
