package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	macPlatform = Platform{OS: OSDarwin, Home: "/Users/u", TempDir: "/private/tmp"}
	linPlatform = Platform{OS: OSLinux, Home: "/home/u", TempDir: "/tmp"}
	winPlatform = Platform{
		OS: OSWindows, Home: `C:\Users\u`, LocalAppData: `C:\Users\u\AppData\Local`, TempDir: `C:\Temp`,
	}
	macOnly = []string{"homebrew", "xcode-deriveddata", "xcode-archives", "ios-simulator", "vscode-shipit"}
)

func TestDefaultProvidersFor_CacheRoots(t *testing.T) {
	linuxXDG := linPlatform
	linuxXDG.XDGCacheHome = "/home/u/xdg-cache"
	linuxOutside := linPlatform
	linuxOutside.XDGCacheHome = "/mnt/cache"

	tests := []struct {
		name     string
		platform Platform
		provider string
		want     []string
	}{
		{"macOS go-build", macPlatform, "go-build", []string{"~/Library/Caches/go-build"}},
		{"linux go-build default", linPlatform, "go-build", []string{"~/.cache/go-build"}},
		{"linux go-build XDG inside home", linuxXDG, "go-build", []string{"~/xdg-cache/go-build"}},
		{"linux go-build XDG outside home", linuxOutside, "go-build", []string{filepath.Join("/mnt/cache", "go-build")}},
		{"windows go-build", winPlatform, "go-build", []string{"~/AppData/Local/go-build"}},
		{"windows npm", winPlatform, "npm", []string{"~/AppData/Local/npm-cache"}},
		{"windows yarn", winPlatform, "yarn", []string{"~/AppData/Local/Yarn/Cache"}},
		{"windows pnpm", winPlatform, "pnpm", []string{"~/AppData/Local/pnpm/store"}},
		{"windows pip", winPlatform, "pip", []string{"~/AppData/Local/pip/Cache"}},
		{"windows uv", winPlatform, "uv", []string{"~/AppData/Local/uv/cache"}},
		{"windows jetbrains", winPlatform, "jetbrains", []string{"~/AppData/Local/JetBrains"}},
		{"windows gradle", winPlatform, "gradle", []string{"~/.gradle/caches"}},
		{"windows cargo", winPlatform, "cargo", []string{"~/.cargo/registry", "~/.cargo/git"}},
		{"windows go-mod", winPlatform, "go-mod", []string{"~/go/pkg/mod"}},
		{"windows edge", winPlatform, "edge", []string{
			"~/AppData/Local/Microsoft/Edge/User Data/Default/Cache",
			"~/AppData/Local/Microsoft/Edge/User Data/Default/Code Cache",
		}},
		{"linux gh honours XDG", linuxXDG, "gh", []string{"~/xdg-cache/gh"}},
		{"linux huggingface honours XDG", linuxOutside, "huggingface", []string{filepath.Join("/mnt/cache", "huggingface", "hub")}},
		{"macOS gh", macPlatform, "gh", []string{"~/.cache/gh"}},
		{"windows huggingface", winPlatform, "huggingface", []string{"~/.cache/huggingface/hub"}},
		{"linux pnpm", linPlatform, "pnpm", []string{"~/.local/share/pnpm/store"}},
		{"linux docker desktop", linPlatform, "docker", []string{"~/.docker/desktop"}},
		{"linux edge", linPlatform, "edge", []string{"~/.cache/microsoft-edge"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := DefaultProvidersFor(tt.platform)[tt.provider]
			require.True(t, ok)
			assert.Equal(t, tt.want, p.Paths)
		})
	}
}

func TestDefaultProvidersFor_WindowsLocalAppDataOutsideHome(t *testing.T) {
	p := winPlatform
	p.LocalAppData = `D:\Apps\Local`

	got := DefaultProvidersFor(p)["go-build"].Paths

	require.Len(t, got, 1)
	assert.Equal(t, filepath.Join(`D:\Apps\Local`, "go-build"), got[0])
}

func TestDefaultProvidersFor_MacOnlyProvidersAbsentElsewhere(t *testing.T) {
	assert.Contains(t, DefaultProvidersFor(macPlatform), "xcode-deriveddata")
	for _, p := range []Platform{linPlatform, winPlatform} {
		defaults := DefaultProvidersFor(p)
		for _, name := range macOnly {
			assert.NotContains(t, defaults, name, "%s on %s", name, p.OS)
		}
	}
	for _, name := range macOnly {
		assert.Contains(t, DefaultProvidersFor(macPlatform), name)
	}
}

func TestDefaultProvidersFor_NoMacPathsOffMac(t *testing.T) {
	for _, p := range []Platform{linPlatform, winPlatform} {
		for name, prov := range DefaultProvidersFor(p) {
			for _, path := range prov.Paths {
				assert.NotContains(t, path, "Library", "%s on %s", name, p.OS)
				assert.NotContains(t, path, "private/tmp", "%s on %s", name, p.OS)
			}
		}
	}
}

func TestDefaultProvidersFor_WindowsOmitsUnixOnlyTools(t *testing.T) {
	defaults := DefaultProvidersFor(winPlatform)
	assert.NotContains(t, defaults, "lima")
	assert.NotContains(t, defaults, "gh")
	assert.Contains(t, defaults, "docker")
}

func TestDefaultProvidersFor_SailDirsUsesOSTempDir(t *testing.T) {
	tests := map[string]Platform{"mac": macPlatform, "linux": linPlatform, "windows": winPlatform}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			sail := DefaultProvidersFor(p)["sail-dirs"]
			assert.Equal(t, []string{filepath.Join(p.TempDir, "sail*")}, sail.Paths)
			assert.False(t, sail.Enabled)
		})
	}
}

func TestDefaultConfigFor_ValidOnEveryOS(t *testing.T) {
	for _, p := range []Platform{macPlatform, linPlatform, winPlatform} {
		t.Run(p.OS, func(t *testing.T) {
			require.NoError(t, DefaultConfigFor(p).Validate())
		})
	}
}

func TestConfig_EnabledProviders_SkipsProvidersOfOtherOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, "xcode")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	providers := map[string]Provider{
		"xcode-deriveddata": {Enabled: true, Paths: []string{dir}, MaxSize: "1G"},
		"custom":            {Enabled: true, Paths: []string{dir}, MaxSize: "1G"},
	}

	onMac := &Config{Providers: providers, goos: OSDarwin}
	onLinux := &Config{Providers: providers, goos: OSLinux}

	assert.Equal(t, []string{"custom", "xcode-deriveddata"}, onMac.EnabledProviders())
	assert.Equal(t, []string{"custom"}, onLinux.EnabledProviders())
	assert.Equal(t, []string{"custom"}, onLinux.AllEnabledProviders())
}

func savedConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	return path
}

func TestLoader_ConfigSavedOnMacLoadsOnOtherOSes(t *testing.T) {
	path := savedConfig(t, `version: "1"
providers:
  go-build:
    enabled: true
    max_size: 4G
    paths:
      - ~/Library/Caches/go-build
  xcode-deriveddata:
    enabled: true
    max_size: 1G
    paths:
      - ~/Library/Developer/Xcode/DerivedData
`)
	for _, p := range []Platform{linPlatform, winPlatform} {
		t.Run(p.OS, func(t *testing.T) {
			loader := NewLoader()
			loader.SetConfigPath(path)
			loader.SetPlatform(p)
			loader.pathsExist = func([]string) bool { return false }

			cfg, err := loader.Load()
			require.NoError(t, err)

			assert.Equal(t, DefaultProvidersFor(p)["go-build"].Paths, cfg.Providers["go-build"].Paths,
				"another OS's default paths give way to this OS's defaults")
			assert.Equal(t, "4G", cfg.Providers["go-build"].MaxSize, "user limits are kept")
			assert.False(t, cfg.Applies("xcode-deriveddata"))
			assert.Contains(t, cfg.Providers, "xcode-deriveddata", "entry stays for the Mac that shares the file")
		})
	}
}

func TestLoader_CustomPathsSurviveOnEveryOS(t *testing.T) {
	path := savedConfig(t, `version: "1"
providers:
  go-build:
    enabled: true
    max_size: 4G
    paths:
      - /data/go-cache
`)
	for _, p := range []Platform{macPlatform, linPlatform, winPlatform} {
		loader := NewLoader()
		loader.SetConfigPath(path)
		loader.SetPlatform(p)
		loader.pathsExist = func([]string) bool { return false }

		cfg, err := loader.Load()
		require.NoError(t, err)

		assert.Equal(t, []string{"/data/go-cache"}, cfg.Providers["go-build"].Paths, p.OS)
	}
}

func TestLoader_DefaultsFollowInjectedPlatform(t *testing.T) {
	loader := NewLoader()
	loader.SetConfigPath(filepath.Join(t.TempDir(), "config.yaml"))
	loader.SetPlatform(linPlatform)
	loader.pathsExist = func([]string) bool { return false }

	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, []string{"~/.cache/go-build"}, cfg.Providers["go-build"].Paths)
	assert.NotContains(t, cfg.Providers, "homebrew")
}

func TestExpandTilde_BackslashSeparatorOnlyOnWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	in := `~\AppData\Local\go-build`

	got, err := ExpandTilde(in)

	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		assert.Equal(t, filepath.Join(home, "AppData", "Local", "go-build"), got)
		return
	}
	assert.Equal(t, in, got, "a backslash is an ordinary character off Windows")
}

func TestLoader_SavedConfigResolvesDefaultsOnTheLoadingOS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mac := macPlatform
	mac.TempDir = "/var/folders/ab/cd/T"
	saver := NewLoader()
	saver.SetConfigPath(path)
	saver.SetPlatform(mac)
	cfg := DefaultConfigFor(mac)
	custom := cfg.Providers["npm"]
	custom.MaxSize = "9G"
	cfg.Providers["npm"] = custom
	require.NoError(t, saver.Save(cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "/var/folders", "machine specific defaults stay out of the file")
	assert.NotContains(t, string(raw), "Library/Caches")

	loader := NewLoader()
	loader.SetConfigPath(path)
	loader.SetPlatform(linPlatform)
	loader.pathsExist = func([]string) bool { return false }
	loaded, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, []string{filepath.Join("/tmp", "sail*")}, loaded.Providers["sail-dirs"].Paths)
	assert.Equal(t, []string{"~/.cache/go-build"}, loaded.Providers["go-build"].Paths)
	assert.Equal(t, "9G", loaded.Providers["npm"].MaxSize)
}

func TestIsAbsPortable(t *testing.T) {
	tests := map[string]bool{
		"/var/tmp/sail*":   true,
		`\\server\share\x`: true,
		`C:\Temp\sail*`:    true,
		"C:/Temp/sail*":    true,
		"d:/x":             true,
		"C:":               false,
		"C:rel":            false,
		"relative/path":    false,
		"~/x":              false,
		"":                 false,
	}
	for in, want := range tests {
		assert.Equal(t, want, IsAbsPortable(in), in)
	}
}

func TestValidate_DirPatternAcceptsDriveLetterPaths(t *testing.T) {
	for _, path := range []string{`C:\Temp\sail*`, "D:/work/sail*", `~\tmp\sail*`, "~/tmp/sail*", "/tmp/sail*"} {
		cfg := &Config{Providers: map[string]Provider{
			"sweep": {Type: TypeDirPattern, Paths: []string{path}, MaxSize: "1G"},
		}}
		assert.NoError(t, cfg.Validate(), path)
	}
}

func TestValidate_DirPatternRejectsRelativePaths(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"sweep": {Type: TypeDirPattern, Paths: []string{`tmp\sail*`}, MaxSize: "1G"},
	}}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
}

func TestValidateProtectedEntry_DriveLetterPaths(t *testing.T) {
	tests := []struct {
		path    string
		wantErr string
	}{
		{`C:\Users\other\Data`, ""},
		{"D:/backups/photos", ""},
		{`~\Documents\keep`, ""},
		{`C:\`, "too broad"},
		{`C:\Windows`, "top-level system directory"},
		{`C:\a\..\b`, "path element"},
		{`C:\a\*`, "glob"},
		{`C:Users\x`, "absolute"},
	}
	for _, tt := range tests {
		err := validateProtectedEntry(tt.path, "")
		if tt.wantErr == "" {
			assert.NoError(t, err, tt.path)
			continue
		}
		require.Error(t, err, tt.path)
		assert.Contains(t, err.Error(), tt.wantErr, tt.path)
	}
}

func TestLoader_MacOnlyEntriesSavedWithoutPathsStayValidElsewhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	saver := NewLoader()
	saver.SetConfigPath(path)
	saver.SetPlatform(macPlatform)
	require.NoError(t, saver.Save(DefaultConfigFor(macPlatform)))

	for _, p := range []Platform{linPlatform, winPlatform} {
		loader := NewLoader()
		loader.SetConfigPath(path)
		loader.SetPlatform(p)
		loader.pathsExist = func([]string) bool { return false }

		cfg, err := loader.Load()
		require.NoError(t, err, p.OS)

		require.NoError(t, cfg.Validate(), p.OS)
		assert.Equal(t, []string{"~/Library/Developer/Xcode/DerivedData"}, cfg.Providers["xcode-deriveddata"].Paths)
		assert.False(t, cfg.Applies("xcode-deriveddata"))
	}
}

func TestTempGlob_EscapesMetacharactersInTempDir(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "a [PC] b")
	require.NoError(t, os.MkdirAll(filepath.Join(odd, "sail-1"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a P b", "sail-2"), 0o750))

	matches, err := filepath.Glob(Platform{TempDir: odd}.tempGlob("sail*"))

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(odd, "sail-1")}, matches)
}

func TestDefaultProvidersFor_NoPathContainsAProtectedEntry(t *testing.T) {
	for _, p := range []Platform{macPlatform, linPlatform, winPlatform} {
		for name, prov := range DefaultProvidersFor(p) {
			for _, path := range prov.Paths {
				for _, protected := range DefaultProtected() {
					assert.False(t, strings.HasPrefix(protected+"/", strings.TrimRight(path, "/")+"/"),
						"%s on %s: %q would be skipped as containing protected %q", name, p.OS, path, protected)
				}
			}
		}
	}
}

func TestLoader_LegacySailDefaultFollowsTheOSTempDir(t *testing.T) {
	path := savedConfig(t, `version: "1"
providers:
  sail-dirs:
    enabled: true
    type: dir-pattern
    max_size: 20G
    min_idle: 2h
    paths:
      - /private/tmp/sail*
`)
	tests := map[string]struct {
		platform Platform
		want     string
	}{
		"linux":                         {linPlatform, filepath.Join("/tmp", "sail*")},
		"windows":                       {winPlatform, filepath.Join(`C:\Temp`, "sail*")},
		"macOS keeps the explicit path": {macPlatform, "/private/tmp/sail*"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			loader := NewLoader()
			loader.SetConfigPath(path)
			loader.SetPlatform(tt.platform)
			loader.pathsExist = func([]string) bool { return false }

			cfg, err := loader.Load()
			require.NoError(t, err)

			assert.Equal(t, []string{tt.want}, cfg.Providers["sail-dirs"].Paths)
			assert.True(t, cfg.Providers["sail-dirs"].Enabled)
		})
	}
}

func TestLoader_ExistingPathEqualToForeignDefaultIsKept(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cache", "pip"), 0o750))
	path := savedConfig(t, `version: "1"
providers:
  pip:
    paths: [~/.cache/pip]
  lima:
    paths: [~/.cache/lima]
`)
	loader := NewLoader()
	loader.SetConfigPath(path)
	loader.SetPlatform(Platform{OS: OSDarwin, Home: home})

	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, []string{"~/.cache/pip"}, cfg.Providers["pip"].Paths, "narrowed on purpose and present here")
	assert.Equal(t, []string{"~/Library/Caches/lima"}, cfg.Providers["lima"].Paths, "absent here, so another OS's default")
}

func TestDefaultProvidersFor_RelativeTempDirFallsBackToTmp(t *testing.T) {
	p := linPlatform
	p.TempDir = "tmp"

	cfg := DefaultConfigFor(p)

	require.NoError(t, cfg.Validate())
	assert.Equal(t, []string{filepath.Join("/tmp", "sail*")}, cfg.Providers["sail-dirs"].Paths)
}

func TestDefaultProvidersFor_MacOSIgnoresXDGDataHome(t *testing.T) {
	p := macPlatform
	p.XDGDataHome = "/Users/u/xdg-data"

	assert.Equal(t, []string{"~/.local/share/mise"}, DefaultProvidersFor(p)["mise"].Paths)
}
