//go:build linux

package osshim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessCwdsFromProc(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "42"), 0o750))
	require.NoError(t, os.Symlink("/work/proj", filepath.Join(root, "42", "cwd")))

	cwds, err := processCwdsFromProc(context.Background(), root, []int{42, 43})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{42: "/work/proj"}, cwds)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = processCwdsFromProc(ctx, root, []int{42})
	assert.Error(t, err)
}
