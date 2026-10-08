package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type miseFixture struct {
	root   string
	node   string
	python string
	marker string
}

func newMiseFixture(t *testing.T) miseFixture {
	t.Helper()
	root := t.TempDir()
	f := miseFixture{
		root:   root,
		node:   filepath.Join(root, "installs", "node", "20.0.0"),
		python: filepath.Join(root, "installs", "python", "3.11.2"),
		marker: filepath.Join(t.TempDir(), "yes-called"),
	}
	writeAged(t, filepath.Join(f.node, "bin", "node"), 3000, time.Hour)
	writeAged(t, filepath.Join(f.python, "bin", "python"), 1000, time.Hour)
	writeAged(t, filepath.Join(root, "installs", "node", "22.1.0", "bin", "node"), 500, time.Hour)
	return f
}

func (f miseFixture) listing() string {
	return "rm -rf " + f.node + "\nrm -rf " + f.python + "\n"
}

func newMise(t *testing.T, root string, cfg config.Provider) *MiseProvider {
	t.Helper()
	cfg.Enabled = true
	cfg.Paths = []string{root}
	if cfg.MaxSize == "" {
		cfg.MaxSize = "1G"
	}
	if cfg.MaxAge == "" {
		cfg.MaxAge = "30d"
	}
	p, err := NewMiseProvider("mise", cfg)
	require.NoError(t, err)
	p.procLines = func(context.Context) ([]string, error) { return nil, nil }
	p.busy = nil
	p.protected = nil
	return p
}

func TestMise_DryRunListsVersionsAndSizesAndDeletesNothing(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {Touch: f.marker},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart, DryRun: true})

	require.NoError(t, err)
	assert.Empty(t, res.SkipReason)
	assert.Contains(t, res.Output, "would prune: node@20.0.0 (")
	assert.Contains(t, res.Output, "would prune: python@3.11.2 (")
	assert.NotContains(t, res.Output, "size unknown")
	assert.Equal(t, int64(4000), res.BytesCleaned)
	require.Len(t, res.Entries, 2)
	assert.Equal(t, "node@20.0.0", res.Entries[0].Detail)
	assert.Equal(t, int64(3000), res.Entries[0].Size)
	assert.DirExists(t, f.node)
	assert.DirExists(t, f.python)
	assert.NoFileExists(t, f.marker)
}

func TestMise_RealRunCallsPruneYesOnce(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes": {
				Remove: []string{f.node}, Touch: f.marker,
				IfExists: f.marker, Then: &fakeReply{Stderr: "called twice", Exit: 9},
			},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

	require.NoError(t, err)
	assert.FileExists(t, f.marker)
	assert.NoDirExists(t, f.node)
	assert.DirExists(t, f.python, "mise kept it, bilgie reports it")
	assert.Contains(t, res.Output, "pruned: node@20.0.0 (")
	assert.Contains(t, res.Output, "kept: python@3.11.2")
	assert.Equal(t, int64(3000), res.BytesCleaned)
	assert.Equal(t, 1, res.SkippedEntries)
	require.Len(t, res.Entries, 1)
	assert.DirExists(t, filepath.Join(f.root, "installs", "node", "22.1.0"))
}

func TestMise_NothingToPruneDoesNotCallPrune(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --yes": {Touch: f.marker}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeFull})

	require.NoError(t, err)
	assert.Contains(t, res.Output, "no unused tool versions")
	assert.NoFileExists(t, f.marker)
}

func TestMise_FullModeBehavesLikeSmart(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {Remove: []string{f.node, f.python}},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeFull})

	require.NoError(t, err)
	assert.Equal(t, int64(4000), res.BytesCleaned)
	assert.NoDirExists(t, f.python)
}

func TestMise_AtVersionListing(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stdout: "node@20.0.0\nghost@1.0\n"}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Contains(t, res.Output, "would prune: node@20.0.0 (")
	assert.Contains(t, res.Output, "would prune: ghost@1.0 (size unknown)")
}

func TestMise_ListingErrorSkipsWithoutDeleting(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stderr: "config error: boom\nmore", Exit: 1},
			"prune --yes":     {Touch: f.marker, Remove: []string{f.node}},
		},
	})
	p := newMise(t, f.root, config.Provider{})
	writeAged(t, filepath.Join(f.root, "downloads", "x", "old.tgz"), 10, 90*24*time.Hour)

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

	require.NoError(t, err)
	assert.Contains(t, res.SkipReason, "mise prune --dry-run failed")
	assert.Contains(t, res.SkipReason, "config error: boom")
	assert.NoFileExists(t, f.marker)
	assert.DirExists(t, f.node)
	assert.FileExists(t, filepath.Join(f.root, "downloads", "x", "old.tgz"))
}

func TestMise_UnparseableOutputSkips(t *testing.T) {
	for name, stdout := range map[string]string{
		"prose":            "Pruning is a state of mind\n",
		"outside installs": "rm -rf /usr/local/bin/node\n",
		"too deep":         "rm -rf INSTALLS/node/20/bin\n",
		"too shallow":      "rm -rf INSTALLS/node\n",
		"installs itself":  "rm -rf INSTALLS\n",
		"good then bad":    "rm -rf INSTALLS/node/20.0.0\nwhat\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newMiseFixture(t)
			stdout = strings.ReplaceAll(stdout, "INSTALLS", filepath.Join(f.root, "installs"))
			installFakeTool(t, "mise", fakeToolSpec{
				Replies: map[string]fakeReply{
					"prune --dry-run": {Stdout: stdout},
					"prune --yes":     {Touch: f.marker},
				},
			})
			p := newMise(t, f.root, config.Provider{})

			res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

			require.NoError(t, err)
			assert.Contains(t, res.SkipReason, "cannot parse")
			assert.NoFileExists(t, f.marker)
			assert.DirExists(t, f.node)
		})
	}
}

func TestMise_StderrNoiseIsNotAListing(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stdout: f.listing(), Stderr: "mise WARN something\n"}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Empty(t, res.SkipReason)
	assert.Len(t, res.Entries, 2)
}

func TestMise_PluginGitClonesDoNotCauseSkip(t *testing.T) {
	f := newMiseFixture(t)
	for _, plugin := range []string{"lua", "make", "teleport-ent"} {
		require.NoError(t, os.MkdirAll(filepath.Join(f.root, "plugins", plugin, ".git"), 0o750))
		writeAged(t, filepath.Join(f.root, "plugins", plugin, "bin", "install"), 10, 90*24*time.Hour)
	}
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {Remove: []string{f.node, f.python}},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

	require.NoError(t, err)
	assert.Empty(t, res.SkipReason)
	assert.NoDirExists(t, f.node)
	for _, plugin := range []string{"lua", "make", "teleport-ent"} {
		assert.FileExists(t, filepath.Join(f.root, "plugins", plugin, "bin", "install"))
		assert.DirExists(t, filepath.Join(f.root, "plugins", plugin, ".git"))
	}
}

func TestMise_RunningManagedBinarySkips(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Touch: f.marker},
			"prune --yes":     {Touch: f.marker},
		},
	})
	p := newMise(t, f.root, config.Provider{})
	bin := filepath.Join(f.root, "installs", "node", "22.1.0", "bin", "node")
	p.procLines = func(context.Context) ([]string, error) {
		return []string{"/usr/bin/true", bin + " server.js"}, nil
	}

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

	require.NoError(t, err)
	assert.Contains(t, res.SkipReason, "mise-managed binary is running")
	assert.NoFileExists(t, f.marker, "mise must not even be asked")
}

func TestMise_ProcessListFailureSkips(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{Replies: map[string]fakeReply{"prune --dry-run": {Touch: f.marker}}})
	p := newMise(t, f.root, config.Provider{})
	p.procLines = func(context.Context) ([]string, error) { return nil, assert.AnError }

	res, err := p.Clean(context.Background(), CleanOptions{})

	require.NoError(t, err)
	assert.Contains(t, res.SkipReason, "cannot list processes")
	assert.NoFileExists(t, f.marker)
}

func TestMise_RunningMiseSkips(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{Replies: map[string]fakeReply{"prune --dry-run": {Touch: f.marker}}})
	p := newMise(t, f.root, config.Provider{})
	p.procLines = func(context.Context) ([]string, error) { return []string{"/opt/homebrew/bin/mise run dev"}, nil }

	res, err := p.Clean(context.Background(), CleanOptions{})

	require.NoError(t, err)
	assert.Equal(t, "mise is running", res.SkipReason)
	assert.NoFileExists(t, f.marker)
}

func TestMise_ProcessMentioningMiseDirIsNotBusy(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{Replies: map[string]fakeReply{"prune --dry-run": {}}})
	p := newMise(t, f.root, config.Provider{})
	p.procLines = func(context.Context) ([]string, error) {
		return []string{"vim " + filepath.Join(f.root, "..", "mise"), "ls /home/u/.config/mise"}, nil
	}

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Empty(t, res.SkipReason)
}

func TestMise_ListingTimeoutSkips(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {SleepMS: 60000},
			"prune --yes":     {Touch: f.marker},
		},
	})
	p := newMise(t, f.root, config.Provider{})
	p.timeout = 3 * time.Second

	start := time.Now()
	res, err := p.Clean(context.Background(), CleanOptions{})

	require.NoError(t, err)
	assert.Contains(t, res.SkipReason, "timed out")
	assert.Less(t, time.Since(start), 30*time.Second)
	assert.NoFileExists(t, f.marker)
}

func TestMise_PruneTimeoutIsAnError(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {SleepMS: 60000},
		},
	})
	p := newMise(t, f.root, config.Provider{})
	p.timeout = 3 * time.Second

	_, err := p.Clean(context.Background(), CleanOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestMise_PruneFailureIsAnError(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {Stderr: "denied", Exit: 1},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	_, err := p.Clean(context.Background(), CleanOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied")
}

func TestMise_ProtectedPathsAreRefused(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{Replies: map[string]fakeReply{"prune --dry-run": {Touch: f.marker}}})

	for name, protected := range map[string]string{
		"equal":    f.root,
		"inside":   filepath.Join(f.root, "installs"),
		"contains": filepath.Dir(f.root),
	} {
		t.Run(name, func(t *testing.T) {
			p := newMise(t, f.root, config.Provider{})
			p.SetProtected([]string{protected})

			res, err := p.Clean(context.Background(), CleanOptions{})

			require.NoError(t, err)
			assert.Contains(t, res.SkipReason, "protected path")
			assert.NoFileExists(t, f.marker)
		})
	}
}

func TestMise_TrimsDownloadsAndCacheByAgeNotInstalls(t *testing.T) {
	f := newMiseFixture(t)
	cacheDir := t.TempDir()
	oldDownload := filepath.Join(f.root, "downloads", "node", "old.tgz")
	newDownload := filepath.Join(f.root, "downloads", "node", "new.tgz")
	oldCache := filepath.Join(cacheDir, "node", "list.msgpack")
	oldInstall := filepath.Join(f.root, "installs", "node", "22.1.0", "bin", "node")
	oldPlugin := filepath.Join(f.root, "plugins", "lua", "bin", "install")
	for _, path := range []string{oldDownload, oldCache, oldInstall, oldPlugin} {
		writeAged(t, path, 10, 90*24*time.Hour)
	}
	writeAged(t, newDownload, 10, time.Hour)
	installFakeTool(t, "mise", fakeToolSpec{})
	p := newMise(t, f.root, config.Provider{})
	p.paths = []string{f.root, cacheDir}

	res, err := p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart, DryRun: true})
	require.NoError(t, err)
	assert.Contains(t, res.Output, "would delete: "+oldDownload)
	assert.FileExists(t, oldDownload)

	_, err = p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})
	require.NoError(t, err)
	assert.NoFileExists(t, oldDownload)
	assert.NoFileExists(t, oldCache)
	assert.FileExists(t, newDownload)
	assert.FileExists(t, oldInstall)
	assert.FileExists(t, oldPlugin)
}

func TestMise_CleanCmdValidation(t *testing.T) {
	for _, cmd := range []string{"mise ls", "mise", "mise run prune", "mise exec node -- npm prune", "mise prune --dry-run", "mise prune --yes"} {
		_, err := NewMiseProvider("mise", config.Provider{Paths: []string{t.TempDir()}, MaxSize: "1G", CleanCmd: cmd})
		require.Error(t, err, cmd)
	}
	p, err := NewMiseProvider("mise", config.Provider{Paths: []string{t.TempDir()}, MaxSize: "1G"})
	require.NoError(t, err)
	assert.Equal(t, []string{"mise", "prune"}, p.cmdArgs)
	_, isAware := any(p).(ProtectionAware)
	assert.True(t, isAware)
}

func TestMise_TildeListingExpandsToHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "mise")
	writeAged(t, filepath.Join(root, "installs", "node", "18.0.0", "bin", "node"), 700, time.Hour)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stdout: "rm -rf ~/.local/share/mise/installs/node/18.0.0\n"}},
	})
	p := newMise(t, root, config.Provider{})
	p.home = home

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Empty(t, res.SkipReason)
	assert.Equal(t, int64(700), res.BytesCleaned)
}

func TestMise_AtListingCannotEscapeInstalls(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stdout: "node@../../..\n"}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Contains(t, res.SkipReason, "cannot parse")
}

func TestMise_UnknownVersionIsNotCountedAsPruned(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stdout: "ghost@1.0\n"}, "prune --yes": {}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{})

	require.NoError(t, err)
	assert.Contains(t, res.Output, "unverified: ghost@1.0")
	assert.Empty(t, res.Entries)
	assert.Zero(t, res.FilesDeleted)
}

func TestMise_FailedPruneStillAccountsRemovedVersions(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{
			"prune --dry-run": {Stdout: f.listing()},
			"prune --yes":     {Remove: []string{f.node}, Stderr: "half done", Exit: 1},
		},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{})

	require.Error(t, err)
	assert.Equal(t, int64(3000), res.BytesCleaned)
	assert.Contains(t, res.Output, "pruned: node@20.0.0")
}

func TestMise_ListingOnStderrIsRead(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stderr: f.listing()}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	assert.Len(t, res.Entries, 2)
}

func TestMise_DataDirWithoutDownloadsIsNeverTrimmedWhole(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "tracked-configs", "abc")
	writeAged(t, state, 10, 90*24*time.Hour)
	installFakeTool(t, "mise", fakeToolSpec{})
	p := newMise(t, root, config.Provider{})

	_, err := p.Clean(context.Background(), CleanOptions{})

	require.NoError(t, err)
	assert.FileExists(t, state)
}

func TestMise_StderrListingAmongWarningsIsRead(t *testing.T) {
	f := newMiseFixture(t)
	installFakeTool(t, "mise", fakeToolSpec{
		Replies: map[string]fakeReply{"prune --dry-run": {Stderr: "mise WARN deprecated\nmise rm -rf " + f.node + "\n"}},
	})
	p := newMise(t, f.root, config.Provider{})

	res, err := p.Clean(context.Background(), CleanOptions{DryRun: true})

	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "node@20.0.0", res.Entries[0].Detail)
}
