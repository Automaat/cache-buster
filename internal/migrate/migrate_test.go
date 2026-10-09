package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestLegacy_MovesConfigAndStateWithTheirFiles(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "auto:\n  interval: 20m\n")
	writeFile(t, filepath.Join(dirs[1][0], "runs.jsonl"), "{\"tier\":\"ok\"}\n")
	writeFile(t, filepath.Join(dirs[1][0], "first-run-pending"), "dry-run pending\n")
	writeFile(t, filepath.Join(dirs[1][0], "auto.lock"), "")
	var out bytes.Buffer

	Legacy(home, &out)

	assert.Equal(t, "auto:\n  interval: 20m\n", readFile(t, filepath.Join(dirs[0][1], "config.yaml")))
	assert.Equal(t, "{\"tier\":\"ok\"}\n", readFile(t, filepath.Join(dirs[1][1], "runs.jsonl")))
	assert.FileExists(t, filepath.Join(dirs[1][1], "first-run-pending"))
	assert.FileExists(t, filepath.Join(dirs[1][1], "auto.lock"))
	assert.NoDirExists(t, dirs[0][0])
	assert.NoDirExists(t, dirs[1][0])
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "migrated "+dirs[0][0]+" to "+dirs[0][1], lines[0])
	assert.Equal(t, "migrated "+dirs[1][0]+" to "+dirs[1][1], lines[1])
}

func TestLegacy_NothingToMigrateIsSilent(t *testing.T) {
	var out bytes.Buffer

	Legacy(t.TempDir(), &out)

	assert.Empty(t, out.String())
}

func TestLegacy_SecondRunIsANoOp(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(Dirs(home)[0][0], "config.yaml"), "x: 1\n")
	Legacy(home, &bytes.Buffer{})
	var out bytes.Buffer

	Legacy(home, &out)

	assert.Empty(t, out.String())
	assert.Equal(t, "x: 1\n", readFile(t, filepath.Join(Dirs(home)[0][1], "config.yaml")))
}

func TestDir_NeverOverwritesAnExistingNewDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	writeFile(t, filepath.Join(oldDir, "config.yaml"), "old\n")
	writeFile(t, filepath.Join(newDir, "config.yaml"), "new\n")

	moved, err := Dir(oldDir, newDir)

	require.ErrorIs(t, err, ErrNewDirExists)
	assert.ErrorContains(t, err, "by hand")
	assert.False(t, moved)
	assert.Equal(t, "new\n", readFile(t, filepath.Join(newDir, "config.yaml")))
	assert.Equal(t, "old\n", readFile(t, filepath.Join(oldDir, "config.yaml")))
}

func TestDir_EmptyExistingNewDirReceivesTheEntries(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	writeFile(t, filepath.Join(oldDir, "config.yaml"), "old\n")
	require.NoError(t, os.Mkdir(newDir, 0o750))

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "old\n", readFile(t, filepath.Join(newDir, "config.yaml")))
	assert.NoDirExists(t, oldDir)
}

func TestDir_EmptyOldDirNextToANewOneIsSilent(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	require.NoError(t, os.Mkdir(oldDir, 0o750))
	require.NoError(t, os.Mkdir(newDir, 0o750))

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.False(t, moved)
}

func TestDir_SymlinkedLegacyDirMovesTheLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "store", "cfg")
	writeFile(t, filepath.Join(target, "config.yaml"), "managed")
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	require.NoError(t, os.Symlink(filepath.Join("store", "cfg"), oldDir))

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.True(t, moved)
	assert.NoFileExists(t, oldDir)
	assert.Equal(t, "managed", readFile(t, filepath.Join(newDir, "config.yaml")))
	assert.Equal(t, "managed", readFile(t, filepath.Join(target, "config.yaml")), "target untouched")
}

func TestDir_DanglingSymlinkIsSkippedWithAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), oldDir))

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.Error(t, err)
	assert.False(t, moved)
	assert.NoDirExists(t, filepath.Join(root, "new"))
}

func TestDir_OldDirDeletedMidMigrationIsSilentWithoutANewDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	writeFile(t, filepath.Join(oldDir, "f"), "1")
	t.Cleanup(func() { rename = os.Rename })
	rename = func(string, string) error {
		require.NoError(t, os.RemoveAll(oldDir))
		return &os.LinkError{Op: "rename", Old: oldDir, Err: os.ErrNotExist}
	}

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.NoError(t, err)
	assert.False(t, moved)
}

func TestDir_OldPathThatIsAFileIsSkippedWithAnError(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	writeFile(t, oldDir, "not a dir")

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.ErrorContains(t, err, "not a directory")
	assert.False(t, moved)
	assert.NoDirExists(t, filepath.Join(root, "new"))
}

func TestLegacy_WarnsAndContinuesPastASkippedDir(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, dirs[0][0], "a file where the config dir should be")
	writeFile(t, filepath.Join(dirs[1][0], "runs.jsonl"), "{}\n")
	var out bytes.Buffer

	Legacy(home, &out)

	assert.Contains(t, out.String(), "warning: not migrating "+dirs[0][0])
	assert.Contains(t, out.String(), "migrated "+dirs[1][0]+" to "+dirs[1][1])
	assert.FileExists(t, dirs[0][0])
	assert.FileExists(t, filepath.Join(dirs[1][1], "runs.jsonl"))
}

func TestDir_LosingTheRenameRaceIsNotAWarning(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	writeFile(t, filepath.Join(oldDir, "f"), "1")
	t.Cleanup(func() { rename = os.Rename })
	rename = func(from, to string) error {
		require.NoError(t, os.Rename(from, to))
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrNotExist}
	}

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.False(t, moved)
	assert.Equal(t, "1", readFile(t, filepath.Join(newDir, "f")))
}

func TestDir_RenameFailureWithTheOldDirStillThereIsReported(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	writeFile(t, filepath.Join(oldDir, "f"), "1")
	t.Cleanup(func() { rename = os.Rename })
	rename = func(string, string) error { return os.ErrNotExist }

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.Error(t, err)
	assert.False(t, moved)
}

func TestTolerateLostRace(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	notExist := &os.PathError{Op: "open", Path: oldDir, Err: os.ErrNotExist}

	require.NoError(t, tolerateLostRace(notExist, oldDir), "old deleted mid-migration, new absent")

	require.NoError(t, os.Mkdir(newDir, 0o750))
	require.NoError(t, tolerateLostRace(notExist, oldDir), "old moved, new present")

	require.NoError(t, os.Mkdir(oldDir, 0o750))
	require.Error(t, tolerateLostRace(notExist, oldDir), "old still present")

	require.NoError(t, os.Remove(oldDir))
	assert.Error(t, tolerateLostRace(os.ErrPermission, oldDir), "other errors pass through")
}

func TestDir_CreatesMissingParentOfTheNewDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	writeFile(t, filepath.Join(oldDir, "f"), "1")
	newDir := filepath.Join(root, "deep", "er", "new")

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "1", readFile(t, filepath.Join(newDir, "f")))
}

func TestDir_LinkBetweenOldAndNewIsSilent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	root := t.TempDir()
	newDir := filepath.Join(root, "new")
	writeFile(t, filepath.Join(newDir, "f"), "1")
	oldDir := filepath.Join(root, "old")
	require.NoError(t, os.Symlink(newDir, oldDir))

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.False(t, moved)
}

func TestDir_StrayLegacyFileNextToANewDirIsSilent(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	writeFile(t, oldDir, "stray")
	require.NoError(t, os.Mkdir(filepath.Join(root, "new"), 0o750))

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.NoError(t, err)
	assert.False(t, moved)
}

func TestDir_RelativeLinkInASymlinkedParentStillResolves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dot", "shared", "legacy", "config.yaml"), "managed")
	require.NoError(t, os.Symlink(filepath.Join(root, "dot"), filepath.Join(root, "cfg")))
	oldDir := filepath.Join(root, "cfg", "old")
	require.NoError(t, os.Symlink(filepath.Join("shared", "legacy"), oldDir))

	moved, err := Dir(oldDir, filepath.Join(root, "cfg", "new"))

	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "managed", readFile(t, filepath.Join(root, "cfg", "new", "config.yaml")))
}

func TestDir_UnreadableFileDoesNotStopTheMove(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not restrict this user")
	}
	home := t.TempDir()
	dirs := Dirs(home)
	secret := filepath.Join(dirs[0][0], "config.yaml")
	writeFile(t, secret, "auto:\n  interval: 20m\n")
	writeFile(t, filepath.Join(dirs[0][0], "root-owned"), "x")
	require.NoError(t, os.Chmod(secret, 0o000))
	require.NoError(t, os.Chmod(filepath.Join(dirs[0][0], "root-owned"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dirs[0][1], "config.yaml"), 0o600) })
	var out bytes.Buffer

	issues := Legacy(home, &out)

	assert.Empty(t, issues)
	assert.NoDirExists(t, dirs[0][0])
	assert.FileExists(t, filepath.Join(dirs[0][1], "config.yaml"))
	assert.FileExists(t, filepath.Join(dirs[0][1], "root-owned"))
	assert.Nil(t, PendingConfig(home))
}

func TestLegacy_BothDirsMergeWhatDoesNotCollide(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, filepath.Join(dirs[1][0], "runs.jsonl"), "old")
	writeFile(t, filepath.Join(dirs[1][0], "first-run-pending"), "p")
	writeFile(t, filepath.Join(dirs[1][0], "auto.lock"), "")
	writeFile(t, filepath.Join(dirs[1][1], "runs.jsonl"), "new")
	writeFile(t, filepath.Join(dirs[1][1], "auto.lock"), "")
	var out bytes.Buffer

	issues := Legacy(home, &out)

	require.Len(t, issues, 1)
	require.ErrorIs(t, issues[0].Err, ErrNewDirExists)
	assert.Equal(t, "new", readFile(t, filepath.Join(dirs[1][1], "runs.jsonl")))
	assert.Equal(t, "old", readFile(t, filepath.Join(dirs[1][0], "runs.jsonl")))
	assert.FileExists(t, filepath.Join(dirs[1][1], "first-run-pending"))
	assert.NoFileExists(t, filepath.Join(dirs[1][0], "auto.lock"))
	assert.Contains(t, out.String(), "warning: not migrating "+dirs[1][0])
	assert.Contains(t, out.String(), "migrated")
}

func TestLegacy_BothDirsWithAConfigOnlyInTheOldOneMovesIt(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "mine")
	writeFile(t, filepath.Join(dirs[0][1], "stray"), "x")

	Legacy(home, &bytes.Buffer{})

	assert.Equal(t, "mine", readFile(t, filepath.Join(dirs[0][1], "config.yaml")))
	assert.Nil(t, PendingConfig(home))
}

func TestLegacy_PersistentFailureWarnsOncePerKindAndDay(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	writeFile(t, dirs[0][0], "a file where the config dir should be")
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	warnings := func() int {
		var out bytes.Buffer
		Legacy(home, &out)
		return strings.Count(out.String(), "warning:")
	}

	assert.Equal(t, 1, warnings())
	for range 5 {
		assert.Zero(t, warnings())
	}
	clock = clock.Add(23 * time.Hour)
	assert.Zero(t, warnings())
	clock = clock.Add(2 * time.Hour)
	assert.Equal(t, 1, warnings(), "a day later")
	assert.Zero(t, warnings())

	require.NoError(t, os.Remove(dirs[0][0]))
	require.NoError(t, os.Symlink(filepath.Join(home, "gone"), dirs[0][0]))
	assert.Equal(t, 1, warnings(), "the failure changed")
}

func TestLegacy_ResolvedFailureIsReportedAgainWhenItReturns(t *testing.T) {
	home := t.TempDir()
	old := Dirs(home)[0][0]
	writeFile(t, old, "file")
	assert.True(t, ShouldReport(home, old, "x"))
	assert.False(t, ShouldReport(home, old, "x"))
	Resolved(home, old)
	assert.True(t, ShouldReport(home, old, "x"))
}

func TestLegacy_ConcurrentFirstRunsPrintMigratedOnce(t *testing.T) {
	for _, linked := range []bool{false, true} {
		if linked && runtime.GOOS == "windows" {
			continue
		}
		for i := range 200 {
			home := t.TempDir()
			dirs := Dirs(home)
			if linked {
				store := filepath.Join(home, "store")
				writeFile(t, filepath.Join(store, "config.yaml"), "x")
				require.NoError(t, os.MkdirAll(filepath.Dir(dirs[0][0]), 0o750))
				require.NoError(t, os.Symlink(store, dirs[0][0]))
			} else {
				writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "x")
			}
			writeFile(t, filepath.Join(dirs[1][0], "runs.jsonl"), "x")
			outs := make([]bytes.Buffer, 6)
			var wg sync.WaitGroup
			for j := range outs {
				wg.Go(func() { Legacy(home, &outs[j]) })
			}
			wg.Wait()
			var all strings.Builder
			for j := range outs {
				all.WriteString(outs[j].String())
			}
			require.Equal(t, 1, strings.Count(all.String(), "migrated "+dirs[0][0]), "run %d linked=%v: %s", i, linked, all.String())
			require.Equal(t, 1, strings.Count(all.String(), "migrated "+dirs[1][0]), "run %d: %s", i, all.String())
			require.NotContains(t, all.String(), "warning")
		}
	}
}

func TestPendingConfig(t *testing.T) {
	home := t.TempDir()
	dirs := Dirs(home)
	assert.Nil(t, PendingConfig(home), "nothing legacy")

	writeFile(t, filepath.Join(dirs[0][0], "config.yaml"), "x")
	p := PendingConfig(home)
	require.NotNil(t, p)
	assert.Contains(t, p.Step(), dirs[0][0])
	assert.Contains(t, p.Step(), dirs[0][1])

	writeFile(t, filepath.Join(dirs[0][1], "config.yaml"), "new")
	assert.Nil(t, PendingConfig(home), "the new config wins")
}

func TestDir_JunctionLikeIrregularModeIsTreatedAsALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot create a junction without tooling")
	}
	root := t.TempDir()
	target := filepath.Join(root, "t")
	writeFile(t, filepath.Join(target, "f"), "1")
	require.NoError(t, os.Symlink(target, filepath.Join(root, "old")))

	moved, err := Dir(filepath.Join(root, "old"), filepath.Join(root, "new"))

	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "1", readFile(t, filepath.Join(target, "f")))
}
