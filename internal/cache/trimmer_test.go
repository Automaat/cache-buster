package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestFile(t *testing.T, path string, size int64, age time.Duration) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))

	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestTrim_DeletesOldFiles(t *testing.T) {
	dir := t.TempDir()

	// Create files with different ages
	createTestFile(t, filepath.Join(dir, "old.txt"), 1000, 40*24*time.Hour)  // 40 days old
	createTestFile(t, filepath.Join(dir, "new.txt"), 1000, 10*24*time.Hour)  // 10 days old
	createTestFile(t, filepath.Join(dir, "newer.txt"), 1000, 5*24*time.Hour) // 5 days old

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 10000, // Large enough to not trigger size-based deletion
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1000), result.FreedBytes)
	assert.Equal(t, int64(1), result.DeletedCount)

	// Verify old file deleted, others remain
	assert.NoFileExists(t, filepath.Join(dir, "old.txt"))
	assert.FileExists(t, filepath.Join(dir, "new.txt"))
	assert.FileExists(t, filepath.Join(dir, "newer.txt"))
}

func TestTrim_DeletesUntilUnderMaxSize(t *testing.T) {
	dir := t.TempDir()

	// Create files totaling 3000 bytes
	createTestFile(t, filepath.Join(dir, "oldest.txt"), 1000, 20*24*time.Hour)
	createTestFile(t, filepath.Join(dir, "middle.txt"), 1000, 10*24*time.Hour)
	createTestFile(t, filepath.Join(dir, "newest.txt"), 1000, 5*24*time.Hour)

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 2000,                // Target: 2000 * 0.9 = 1800 bytes
		MaxAge:  60 * 24 * time.Hour, // No files qualify for age-based deletion
		DryRun:  false,
	})

	require.NoError(t, err)
	// Should delete oldest and middle to get under 1800
	assert.Equal(t, int64(2000), result.FreedBytes)
	assert.Equal(t, int64(2), result.DeletedCount)

	// Verify oldest files deleted
	assert.NoFileExists(t, filepath.Join(dir, "oldest.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "middle.txt"))
	assert.FileExists(t, filepath.Join(dir, "newest.txt"))
}

func TestTrim_DryRun(t *testing.T) {
	dir := t.TempDir()

	createTestFile(t, filepath.Join(dir, "old.txt"), 1000, 40*24*time.Hour)

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 10000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  true,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1000), result.FreedBytes)
	assert.Equal(t, int64(1), result.DeletedCount)
	assert.Contains(t, result.Output, "would delete")

	// File should still exist
	assert.FileExists(t, filepath.Join(dir, "old.txt"))
}

func TestTrim_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 1000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.FreedBytes)
	assert.Equal(t, int64(0), result.DeletedCount)
	assert.Contains(t, result.Output, "no files found")
}

func TestTrim_AlreadyUnderLimit(t *testing.T) {
	dir := t.TempDir()

	// All files are recent and under size limit
	createTestFile(t, filepath.Join(dir, "recent.txt"), 100, 1*24*time.Hour)

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 10000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.FreedBytes)
	assert.Equal(t, int64(0), result.DeletedCount)
	assert.FileExists(t, filepath.Join(dir, "recent.txt"))
}

func TestTrim_CombinesAgeAndSize(t *testing.T) {
	dir := t.TempDir()

	// Files: 1 old (age), 2 recent but over size
	createTestFile(t, filepath.Join(dir, "ancient.txt"), 500, 60*24*time.Hour) // Age-based deletion
	createTestFile(t, filepath.Join(dir, "old.txt"), 500, 15*24*time.Hour)     // Size-based (oldest remaining)
	createTestFile(t, filepath.Join(dir, "recent.txt"), 500, 5*24*time.Hour)   // Keep

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 1000, // Target: 900 bytes after buffer
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	// ancient.txt deleted by age, old.txt deleted by size
	assert.Equal(t, int64(1000), result.FreedBytes)
	assert.Equal(t, int64(2), result.DeletedCount)

	assert.NoFileExists(t, filepath.Join(dir, "ancient.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "old.txt"))
	assert.FileExists(t, filepath.Join(dir, "recent.txt"))
}

func TestTrim_ContextCancellation(t *testing.T) {
	dir := t.TempDir()

	createTestFile(t, filepath.Join(dir, "file1.txt"), 1000, 40*24*time.Hour)
	createTestFile(t, filepath.Join(dir, "file2.txt"), 1000, 41*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := Trim(ctx, []string{dir}, TrimOptions{
		MaxSize: 100,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, result.Output, "interrupted")
}

func TestTrim_MultiplePaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	createTestFile(t, filepath.Join(dir1, "old1.txt"), 500, 40*24*time.Hour)
	createTestFile(t, filepath.Join(dir2, "old2.txt"), 500, 45*24*time.Hour)

	result, err := Trim(context.Background(), []string{dir1, dir2}, TrimOptions{
		MaxSize: 10000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1000), result.FreedBytes)
	assert.Equal(t, int64(2), result.DeletedCount)
}

func TestTrim_DeleteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	dir := t.TempDir()

	// Create old file
	oldFile := filepath.Join(dir, "old.txt")
	createTestFile(t, oldFile, 1000, 40*24*time.Hour)

	// Make directory read-only so delete fails
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 10000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(0), result.FreedBytes)
	assert.Equal(t, int64(0), result.DeletedCount)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, ReasonPermissionDenied, result.Errors[0].Reason)
}

func TestTrim_CarriesForwardScanWarnings(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	dir := t.TempDir()

	// Create accessible file
	createTestFile(t, filepath.Join(dir, "accessible.txt"), 100, 1*24*time.Hour)

	// Create inaccessible subdirectory
	inaccessible := filepath.Join(dir, "noaccess")
	require.NoError(t, os.Mkdir(inaccessible, 0o000))
	t.Cleanup(func() { _ = os.Chmod(inaccessible, 0o750) })

	result, err := Trim(context.Background(), []string{dir}, TrimOptions{
		MaxSize: 10000,
		MaxAge:  30 * 24 * time.Hour,
		DryRun:  false,
	})

	require.NoError(t, err)
	// Should have warning from scan
	assert.NotEmpty(t, result.Errors)
}
