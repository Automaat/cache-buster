package config

import "maps"

const currentVersion = "1"

// DefaultProviders returns builtin provider definitions.
func DefaultProviders() map[string]Provider {
	all := make(map[string]Provider)
	for _, group := range []map[string]Provider{
		goProviders(),
		jsProviders(),
		systemProviders(),
		xcodeProviders(),
		otherProviders(),
	} {
		maps.Copy(all, group)
	}
	return all
}

func goProviders() map[string]Provider {
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
	}
}

func jsProviders() map[string]Provider {
	return map[string]Provider{
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
		"pnpm": {
			Enabled:  true,
			Paths:    []string{"~/.local/share/pnpm/store", "~/Library/pnpm/store"},
			MaxSize:  "5G",
			MaxAge:   "30d",
			CleanCmd: "pnpm store prune",
		},
	}
}

func systemProviders() map[string]Provider {
	return map[string]Provider{
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
		"docker": {
			Enabled:  true,
			Paths:    []string{"~/Library/Containers/com.docker.docker"},
			MaxSize:  "50G",
			MaxAge:   "30d",
			CleanCmd: "docker system prune -af --volumes",
		},
	}
}

// xcodeProviders are macOS-only: Xcode and iOS Simulator caches.
func xcodeProviders() map[string]Provider {
	return map[string]Provider{
		"xcode-deriveddata": {
			Enabled:  true,
			Paths:    []string{"~/Library/Developer/Xcode/DerivedData"},
			MaxSize:  "20G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"xcode-archives": {
			Enabled:  true,
			Paths:    []string{"~/Library/Developer/Xcode/Archives"},
			MaxSize:  "10G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"ios-simulator": {
			Enabled:  true,
			Paths:    []string{"~/Library/Developer/CoreSimulator/Caches"},
			MaxSize:  "10G",
			MaxAge:   "30d",
			CleanCmd: "xcrun simctl delete unavailable",
		},
	}
}

func otherProviders() map[string]Provider {
	return map[string]Provider{
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
		"cargo": {
			Enabled:  true,
			Paths:    []string{"~/.cargo/registry", "~/.cargo/git"},
			MaxSize:  "5G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"gradle": {
			Enabled:  true,
			Paths:    []string{"~/.gradle/caches"},
			MaxSize:  "10G",
			MaxAge:   "30d",
			CleanCmd: "",
		},
		"pip": {
			Enabled:  true,
			Paths:    []string{"~/.cache/pip", "~/Library/Caches/pip"},
			MaxSize:  "3G",
			MaxAge:   "30d",
			CleanCmd: "pip cache purge",
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
