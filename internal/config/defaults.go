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
		tempDirProviders(),
		extraCacheProviders(),
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
			CleanCmd: "brew cleanup -s",
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
			CleanCmd: "docker system prune -af",
		},
		"docker-volumes": {
			Enabled:  false,
			Paths:    []string{"~/Library/Containers/com.docker.docker"},
			MaxSize:  "50G",
			CleanCmd: "docker volume prune -f",
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

// extraCacheProviders covers browser, ML, tooling and toolchain caches.
func extraCacheProviders() map[string]Provider {
	cache := func(path, maxSize string) Provider {
		return Provider{
			Enabled: true,
			Paths:   []string{path},
			MaxSize: maxSize,
			MaxAge:  "30d",
		}
	}
	// Opt-in: entry mtime records download time, not use, so a daily-use
	// entry can be evicted before an idle one.
	optIn := func(path, maxSize string) Provider {
		p := cache(path, maxSize)
		p.Enabled = false
		return p
	}
	return map[string]Provider{
		"edge":    cache("~/Library/Caches/Microsoft Edge", "3G"),
		"vivaldi": cache("~/Library/Caches/Vivaldi", "3G"),
		// hub only: ~/.cache/huggingface also holds the login token.
		"huggingface": optIn("~/.cache/huggingface/hub", "20G"),
		"playwright":  optIn("~/Library/Caches/ms-playwright", "5G"),
		"lima":        cache("~/Library/Caches/lima", "10G"),
		"gh":          cache("~/.cache/gh", "1G"),
		// chrome-profile-* holds browser logins and cookies.
		"chrome-devtools-mcp": {
			Enabled:      false,
			Paths:        []string{"~/.cache/chrome-devtools-mcp"},
			MaxSize:      "2G",
			MaxAge:       "30d",
			SkipPrefixes: []string{"chrome-profile-"},
		},
		"vscode-shipit": cache("~/Library/Caches/com.microsoft.VSCode.ShipIt", "1G"),
		// Toolchains are not a plain cache, so this stays opt-in.
		"rustup": {
			Enabled:  false,
			Paths:    []string{"~/.rustup/toolchains"},
			MaxSize:  "10G",
			CleanCmd: "rustup toolchain uninstall",
		},
	}
}

// DefaultConfig returns config with all default providers.
func DefaultConfig() *Config {
	return &Config{
		Version:   currentVersion,
		Providers: DefaultProviders(),
		Auto:      DefaultAuto(),
		Protected: DefaultProtected(),
	}
}

// tempDirProviders are opt-in: they delete whole directories, so they stay
// disabled until the user enables them.
func tempDirProviders() map[string]Provider {
	return map[string]Provider{
		"sail-dirs": {
			Enabled: false,
			Type:    TypeDirPattern,
			Paths:   []string{"/private/tmp/sail*"},
			MaxSize: "20G",
			MinIdle: "2h",
		},
	}
}
