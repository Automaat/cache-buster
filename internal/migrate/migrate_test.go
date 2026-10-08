package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

	require.NoError(t, err)
	assert.False(t, moved)
	assert.Equal(t, "new\n", readFile(t, filepath.Join(newDir, "config.yaml")))
	assert.Equal(t, "old\n", readFile(t, filepath.Join(oldDir, "config.yaml")))
}

func TestDir_EmptyExistingNewDirBlocksTheMove(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	writeFile(t, filepath.Join(oldDir, "config.yaml"), "old\n")
	require.NoError(t, os.Mkdir(newDir, 0o750))

	moved, err := Dir(oldDir, newDir)

	require.NoError(t, err)
	assert.False(t, moved)
	assert.FileExists(t, filepath.Join(oldDir, "config.yaml"))
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

func TestDir_UnreadableContentIsSkippedWithAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not restrict this user")
	}
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	secret := filepath.Join(oldDir, "config.yaml")
	writeFile(t, secret, "x\n")
	require.NoError(t, os.Chmod(secret, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secret, 0o600) })

	moved, err := Dir(oldDir, filepath.Join(root, "new"))

	require.Error(t, err)
	assert.False(t, moved)
	assert.DirExists(t, oldDir)
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

	require.Error(t, tolerateLostRace(notExist, oldDir, newDir), "old and new both missing")

	require.NoError(t, os.Mkdir(newDir, 0o750))
	require.NoError(t, tolerateLostRace(notExist, oldDir, newDir), "old moved, new present")

	require.NoError(t, os.Mkdir(oldDir, 0o750))
	require.Error(t, tolerateLostRace(notExist, oldDir, newDir), "old still present")

	require.NoError(t, os.Remove(oldDir))
	assert.Error(t, tolerateLostRace(os.ErrPermission, oldDir, newDir), "other errors pass through")
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
