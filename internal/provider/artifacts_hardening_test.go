package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTimes(t *testing.T, path string, atime, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(path, atime, mtime))
}

func TestProjectArtifacts_ArtifactInUseKeepsIdleProject(t *testing.T) {
	now := time.Now()
	old := now.Add(-90 * day)
	tests := []struct {
		name  string
		setup func(t *testing.T, h *artifactHarness)
		keep  []string
	}{
		{"executed release binary", func(t *testing.T, h *artifactHarness) {
			t.Helper()
			rustProject(t, h.path("api"), 100, 90*day)
			bin := h.path("api", "target", "release", "tool")
			writeFile(t, bin, "elf")
			ageTree(t, h.path("api"), 90*day)
			setTimes(t, bin, now, old)
		}, []string{"api", "target"}},
		{"executed debug binary under a target triple", func(t *testing.T, h *artifactHarness) {
			t.Helper()
			rustProject(t, h.path("api"), 100, 90*day)
			bin := h.path("api", "target", "aarch64-unknown-linux-gnu", "debug", "tool")
			writeFile(t, bin, "elf")
			ageTree(t, h.path("api"), 90*day)
			setTimes(t, bin, now, old)
		}, []string{"api", "target"}},
		{"artifact rebuilt recently", func(t *testing.T, h *artifactHarness) {
			t.Helper()
			rustProject(t, h.path("api"), 100, 90*day)
			setTimes(t, h.path("api", "target", "debug", "deps", "big.rlib"), old, now)
			setTimes(t, h.path("api", "target", "debug", "deps"), old, now)
		}, []string{"api", "target"}},
		{"node bin script executed", func(t *testing.T, h *artifactHarness) {
			t.Helper()
			nodeProject(t, h.path("web"), 100, 90*day)
			script := h.path("web", "node_modules", "tool", "cli.js")
			writeFile(t, script, "x")
			bin := h.path("web", "node_modules", ".bin")
			require.NoError(t, os.MkdirAll(bin, 0o750))
			if err := os.Symlink(filepath.Join("..", "tool", "cli.js"), filepath.Join(bin, "tool")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			ageTree(t, h.path("web"), 90*day)
			setTimes(t, script, now, old)
		}, []string{"web", "node_modules"}},
		{"npm read the lock", func(t *testing.T, h *artifactHarness) {
			t.Helper()
			nodeProject(t, h.path("web"), 100, 90*day)
			lock := h.path("web", "node_modules", ".package-lock.json")
			writeFile(t, lock, "{}")
			ageTree(t, h.path("web"), 90*day)
			setTimes(t, lock, now, old)
		}, []string{"web", "node_modules"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newArtifactHarness(t, nil)
			tt.setup(t, h)

			res := h.clean(CleanOptions{Mode: CleanModeFull})

			assert.Empty(t, res.Entries)
			assert.DirExists(t, h.path(tt.keep...))
			assert.Contains(t, res.Output, "in use")
		})
	}
}

func TestProjectArtifacts_UnusedArtifactOfIdleProjectIsRemoved(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	writeFile(t, h.path("api", "target", "release", "tool"), "elf")
	ageTree(t, h.path("api"), 90*day)

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.Len(t, res.Entries, 1)
	assert.NoDirExists(t, h.path("api", "target"))
}

func TestProjectArtifacts_ArtifactUseRecheckedJustBeforeRemoval(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	bin := h.path("api", "target", "release", "tool")
	writeFile(t, bin, "elf")
	ageTree(t, h.path("api"), 90*day)
	h.p.openCheck = func(context.Context, string) (bool, error) {
		now := time.Now()
		return false, os.Chtimes(bin, now, now.Add(-90*day))
	}

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.Empty(t, res.Entries)
	assert.DirExists(t, h.path("api", "target"))
}

func TestProjectArtifacts_RelativePathProcessesBlockRemoval(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		tool    string
		cmd     string
		cwd     func(root, project string) string
		blocked bool
	}{
		{"cd parent and run node app/server.js", "node", "node", "node app/server.js", func(r, _ string) string { return r }, true},
		{"cd parent and run dotted path", "node", "node", "node ./app/server.js", func(r, _ string) string { return r }, true},
		{"sibling dir with dotdot path", "node", "node", "node ../app/server.js", func(r, _ string) string { return filepath.Join(r, "other") }, true},
		{"npm prefix", "node", "npm", "npm --prefix app start", func(r, _ string) string { return r }, true},
		{"npm prefix equals", "node", "npm", "npm --prefix=app start", func(r, _ string) string { return r }, true},
		{"cargo manifest path", "rust", "cargo", "cargo run --manifest-path app/Cargo.toml", func(r, _ string) string { return r }, true},
		{"cargo manifest path equals", "rust", "cargo", "cargo run --manifest-path=app/Cargo.toml", func(r, _ string) string { return r }, true},
		{"unrelated relative path from the parent", "node", "node", "node elsewhere/server.js", func(r, _ string) string { return r }, false},
		{"shell in the parent with a bare tool", "node", "node", "node", func(r, _ string) string { return r }, false},
		{"npm prefix at a sibling", "node", "npm", "npm --prefix elsewhere start", func(r, _ string) string { return r }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newArtifactHarness(t, nil)
			if tt.kind == "node" {
				nodeProject(t, h.path("app"), 100, 90*day)
			} else {
				rustProject(t, h.path("app"), 100, 90*day)
			}
			require.NoError(t, os.MkdirAll(h.path("other"), 0o750))
			root := evalDir(t, h.root)
			proj := evalDir(t, h.path("app"))
			h.p.processes = func(context.Context) ([]toolProcess, error) {
				return []toolProcess{{Tool: tt.tool, CommandLine: tt.cmd, Cwd: tt.cwd(root, proj)}}, nil
			}

			res := h.clean(CleanOptions{Mode: CleanModeFull})

			art := "node_modules"
			if tt.kind == "rust" {
				art = "target"
			}
			if tt.blocked {
				assert.DirExists(t, h.path("app", art))
				assert.Contains(t, res.Output, "is running")
			} else {
				assert.NoDirExists(t, h.path("app", art))
			}
		})
	}
}

func TestProjectArtifacts_TruncatedSampleFailsClosedInGitRepos(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	for i := range sampleLimit + 5 {
		writeFile(t, h.path("api", "docs", fmt.Sprintf("f%05d.md", i)), "x")
	}
	ageTree(t, h.path("api", "docs"), 90*day)
	gitRepo(t, h.path("api"), 90*day)

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.DirExists(t, h.path("api", "target"))
	assert.Contains(t, res.Output, "too many files")
}

func TestProjectArtifacts_SourceDirectoriesNamedLikeArtifactsAreSampled(t *testing.T) {
	tests := []struct{ name, rel string }{
		{"rust module", "src/target/mod.rs"},
		{"node folder", "src/node_modules/x.js"},
		{"venv folder", "src/venv/x.py"},
		{"dot venv folder", "src/.venv/x.py"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newArtifactHarness(t, nil)
			rustProject(t, h.path("api"), 100, 90*day)
			file := h.path("api", filepath.FromSlash(tt.rel))
			writeFile(t, file, "edit")
			ageTree(t, h.path("api"), 90*day)
			now := time.Now()
			setTimes(t, file, now, now)

			res := h.clean(CleanOptions{Mode: CleanModeFull})

			assert.Empty(t, res.Entries)
			assert.DirExists(t, h.path("api", "target"))
		})
	}
}

func TestProjectArtifacts_RecognisedArtifactEditsAreStillIgnored(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	writeFile(t, h.path("api", "node_modules", "x", "y.js"), "x")
	writeFile(t, h.path("api", "package.json"), "{}")
	ageTree(t, h.path("api"), 90*day)
	now := time.Now()
	setTimes(t, h.path("api", "node_modules", "x", "y.js"), now, now)
	setTimes(t, h.path("api", "target", "debug", "deps", "big.rlib"), now.Add(-90*day), now.Add(-90*day))

	sampled, truncated, err := sampleNewest(context.Background(), h.path("api"), sampleLimit)

	require.NoError(t, err)
	assert.False(t, truncated)
	assert.True(t, sampled.Before(now.Add(-day)), "the artifact's own files are not project activity")
}

func TestProjectArtifacts_TrashSweepIsNarrow(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	keep := []string{
		h.path("api", trashPrefix+"keep"),
		h.path("api", trashPrefix+"target-abc123"),
		h.path("api", trashPrefix+"target-ZZZZZZZZ"),
		h.path("api", trashPrefix+"node_modules-abcdefgh"),
		h.path("api", "src", trashPrefix+"target-abcdefgh"),
		h.path("stray", trashPrefix+"target-abcdefgh"),
	}
	for _, dir := range keep {
		writeFile(t, filepath.Join(dir, "precious.txt"), "mine")
	}
	mine := h.path("api", trashPrefix+"target-abcdefgh")
	writeFile(t, filepath.Join(mine, "debug", "half.rlib"), "half")
	ageTree(t, h.path("api"), 90*day)
	ageTree(t, h.path("stray"), 90*day)

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	for _, dir := range keep {
		assert.FileExists(t, filepath.Join(dir, "precious.txt"), dir)
	}
	assert.NoDirExists(t, mine)
	assert.Contains(t, res.Output, "swept: "+mine)
}

func TestProjectArtifacts_EditDuringFirstRemovalBlocksSecondArtifact(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("mix"), 4000, 90*day)
	nodeProject(t, h.path("mix"), 10, 90*day)
	calls := 0
	h.p.processes = func(context.Context) ([]toolProcess, error) {
		calls++
		if calls == 2 {
			now := time.Now()
			return nil, os.Chtimes(h.path("mix", "src", "main.rs"), now, now)
		}
		return nil, nil
	}

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.NoDirExists(t, h.path("mix", "target"), "the first artifact passed every check before the edit")
	assert.DirExists(t, h.path("mix", "node_modules"))
	assert.Contains(t, res.Output, "modified during checks")
}

func TestProjectArtifacts_FailedDeleteIsAccounted(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 4000, 90*day)
	writeFile(t, h.path("api", "target", "locked", "keep.bin"), strings.Repeat("k", 1000))
	ageTree(t, h.path("api"), 90*day)
	require.NoError(t, os.Chmod(h.path("api", "target", "locked"), 0o500))
	t.Cleanup(func() {
		matches, _ := filepath.Glob(h.path("api", trashPrefix+"*", "locked"))
		for _, m := range matches {
			_ = os.Chmod(m, 0o700)
		}
		_ = os.Chmod(h.path("api", "target", "locked"), 0o700)
	})

	h.p.invalidate()
	res, err := h.p.Clean(context.Background(), CleanOptions{Mode: CleanModeFull})

	require.Error(t, err)
	assert.Equal(t, int64(4000+len(artifactTag())), res.BytesCleaned)
	assert.Len(t, res.Entries, 1)
	assert.Equal(t, int64(4000+len(artifactTag())), res.Entries[0].Size)
	assert.Contains(t, res.Output, "error: ")
}

func TestProjectArtifacts_FailedDeleteDoesNotOverEvictInSmartMode(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	h := newArtifactHarness(t, nil)
	h.p.maxSize = 1500
	rustProject(t, h.path("a-old"), 2000, 120*day)
	writeFile(t, h.path("a-old", "target", "locked", "keep.bin"), "k")
	ageTree(t, h.path("a-old"), 120*day)
	require.NoError(t, os.Chmod(h.path("a-old", "target", "locked"), 0o500))
	t.Cleanup(func() {
		matches, _ := filepath.Glob(h.path("a-old", trashPrefix+"*", "locked"))
		for _, m := range matches {
			_ = os.Chmod(m, 0o700)
		}
	})
	nodeProject(t, h.path("b-new"), 1000, 90*day)

	h.p.invalidate()
	_, _ = h.p.Clean(context.Background(), CleanOptions{Mode: CleanModeSmart})

	assert.DirExists(t, h.path("b-new", "node_modules"), "the failed delete freed enough")
}

func TestOwnsTrash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pyproject.toml"), "x")
	tests := []struct {
		name string
		want bool
	}{
		{trashPrefix + "venv-abcd2345", true},
		{trashPrefix + "target-abcd2345", false},
		{trashPrefix + "venv-abcd23", false},
		{trashPrefix + "venv-abcd234567", false},
		{trashPrefix + "venv-ABCD2345", false},
		{trashPrefix + "keep", false},
		{"x" + trashPrefix + "venv-abcd2345", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ownsTrash(dir, tt.name), tt.name)
	}
	assert.True(t, trashPattern.MatchString(trashName("target")))
	assert.True(t, trashPattern.MatchString(trashName(".venv")))
	assert.True(t, trashPattern.MatchString(trashName("node_modules")))
}

func TestArgsReach(t *testing.T) {
	proj := "/code/app"
	tests := []struct {
		name string
		proc toolProcess
		want bool
	}{
		{"relative script", toolProcess{CommandLine: "node app/server.js", Cwd: "/code"}, true},
		{"manifest flag", toolProcess{CommandLine: "cargo run --manifest-path app/Cargo.toml", Cwd: "/code"}, true},
		{"short chdir flag", toolProcess{CommandLine: "cargo -C app build", Cwd: "/code"}, true},
		{"prefix flag", toolProcess{CommandLine: "npm --prefix app start", Cwd: "/code"}, true},
		{"quoted", toolProcess{CommandLine: `sh -c "cd /code; node ./app/x.js"`, Cwd: "/code"}, true},
		{"dotdot", toolProcess{CommandLine: "node ../app/x.js", Cwd: "/code/other"}, true},
		{"bare name is not a path", toolProcess{CommandLine: "node app", Cwd: "/code"}, false},
		{"sibling", toolProcess{CommandLine: "node app2/x.js", Cwd: "/code"}, false},
		{"flag without value", toolProcess{CommandLine: "npm --prefix", Cwd: "/code"}, false},
		{"no cwd", toolProcess{CommandLine: "node app/x.js"}, false},
		{"ancestor cwd alone", toolProcess{CommandLine: "node", Cwd: "/code"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("posix paths")
			}
			assert.Equal(t, tt.want, argsReach(tt.proc, proj, ""))
		})
	}
}

func TestProjectArtifacts_PythonVenvInUseKeepsProject(t *testing.T) {
	on := true
	h := newArtifactHarness(t, func(c *config.Provider) { c.Python = &on })
	pythonProject(t, h.path("ml"), 100, 90*day)
	cfgFile := h.path("ml", ".venv", "pyvenv.cfg")
	setTimes(t, cfgFile, time.Now(), time.Now().Add(-90*day))

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.Empty(t, res.Entries)
	assert.DirExists(t, h.path("ml", ".venv"))
}

func TestSplitQuoted(t *testing.T) {
	assert.Equal(t, []string{"node", "my app/server.js", "x"}, splitQuoted(`node "my app/server.js" x`))
	assert.Equal(t, []string{"npm", "--prefix=my app", "start"}, splitQuoted(`npm --prefix="my app" start`))
	assert.Equal(t, []string{"a", ""}, splitQuoted(`a ''`))
}

func TestArgsReachQuotedAndWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix paths")
	}
	for _, cmd := range []string{`node "my app/server.js"`, `npm --prefix "my app" start`, `npm --prefix="my app" start`, `npm -w "my app" dev`} {
		assert.True(t, argsReach(toolProcess{CommandLine: cmd, Cwd: "/code"}, "/code/my app"), cmd)
	}
}

func TestProjectArtifacts_CustomCargoProfileBinaryKeepsTarget(t *testing.T) {
	h := newArtifactHarness(t, nil)
	rustProject(t, h.path("api"), 100, 90*day)
	bin := h.path("api", "target", "dist", "app")
	writeFile(t, bin, "elf")
	ageTree(t, h.path("api"), 90*day)
	setTimes(t, bin, time.Now(), time.Now().Add(-90*day))

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	assert.Empty(t, res.Entries)
	assert.DirExists(t, h.path("api", "target"))
}

func TestProjectArtifacts_GitMarkerCaseVariants(t *testing.T) {
	when := time.Now().Add(-2 * day)
	writeGit := func(t *testing.T, gitDir string, when time.Time) {
		t.Helper()
		writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(gitDir, "logs", "HEAD"), fmt.Sprintf("0 1 A <a@b.c> %d +0000\tcommit: x\n", when.Unix()))
	}
	t.Run("directory with recent reflog keeps the project", func(t *testing.T) {
		h := newArtifactHarness(t, nil)
		rustProject(t, h.path("api"), 100, 90*day)
		writeGit(t, h.path("api", ".GIT"), when)
		ageTree(t, h.path("api", ".GIT", "logs"), 90*day)
		ageTree(t, h.path("api", ".GIT", "HEAD"), 90*day)
		ageTree(t, h.path("api", ".GIT"), 90*day)
		old := time.Now().Add(-90 * day)
		setTimes(t, h.path("api"), old, old)

		res := h.clean(CleanOptions{Mode: CleanModeFull})

		assert.Empty(t, res.Entries)
		assert.DirExists(t, h.path("api", "target"))
	})
	t.Run("directory is dirty-checked", func(t *testing.T) {
		h := newArtifactHarness(t, nil)
		rustProject(t, h.path("api"), 100, 90*day)
		writeGit(t, h.path("api", ".Git"), time.Now().Add(-90*day))
		ageTree(t, h.path("api"), 90*day)
		old := time.Now().Add(-90 * day)
		require.NoError(t, os.Chtimes(h.path("api", ".Git", "logs", "HEAD"), old, old))
		h.dirty[evalDir(t, h.path("api"))] = true

		res := h.clean(CleanOptions{Mode: CleanModeFull})

		assert.Empty(t, res.Entries)
		assert.Contains(t, res.Output, "uncommitted changes")
	})
	t.Run("worktree-style file points at a recent admin dir", func(t *testing.T) {
		h := newArtifactHarness(t, nil)
		rustProject(t, h.path("wt"), 100, 90*day)
		admin := h.path("admin")
		writeGit(t, admin, when)
		writeFile(t, h.path("wt", ".GIT"), "gitdir: "+admin+"\n")
		ageTree(t, h.path("wt"), 90*day)

		res := h.clean(CleanOptions{Mode: CleanModeFull})

		assert.Empty(t, res.Entries)
		assert.DirExists(t, h.path("wt", "target"))
	})
	t.Run("contents of the git directory are not project activity", func(t *testing.T) {
		h := newArtifactHarness(t, nil)
		rustProject(t, h.path("api"), 100, 90*day)
		gitRepo(t, h.path("api"), 90*day)
		require.NoError(t, os.Rename(h.path("api", ".git"), h.path("api", ".GIT")))
		writeFile(t, h.path("api", ".GIT", "objects", "pack", "p.pack"), "x")
		ageTree(t, h.path("api"), 90*day)
		now := time.Now()
		setTimes(t, h.path("api", ".GIT", "objects", "pack", "p.pack"), now, now)

		sampled, _, err := sampleNewest(context.Background(), h.path("api"), sampleLimit)

		require.NoError(t, err)
		assert.True(t, sampled.Before(now.Add(-day)))
	})
}

func TestProjectArtifacts_ArtifactNamesFollowFilesystemCase(t *testing.T) {
	probe := t.TempDir()
	writeFile(t, filepath.Join(probe, "a"), "x")
	_, err := os.Lstat(filepath.Join(probe, "A"))
	insensitive := err == nil

	h := newArtifactHarness(t, nil)
	writeFile(t, h.path("api", "cargo.toml"), "x")
	writeFile(t, h.path("api", "Target", "CACHEDIR.TAG"), artifactTag())
	writeFile(t, h.path("api", "Target", "debug", "a.rlib"), "data")
	ageTree(t, h.path("api"), 90*day)

	res := h.clean(CleanOptions{Mode: CleanModeFull})

	if insensitive {
		assert.Len(t, res.Entries, 1, "a differently cased spelling is the same directory here")
		assert.NoDirExists(t, h.path("api", "Target"))
		return
	}
	assert.Empty(t, res.Entries, "exact names only on a case-sensitive volume")
	assert.DirExists(t, h.path("api", "Target"))
}
