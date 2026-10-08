package osshim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// linkOrSkip hard-links oldname to newname, skipping the test on file
// systems without hard-link support.
func linkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Link(oldname, newname); err != nil {
		t.Skipf("hard links unsupported here: %v", err)
	}
}

func TestSharedFileID(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	single := filepath.Join(dir, "single")
	require.NoError(t, os.WriteFile(a, []byte("data"), 0o600))
	require.NoError(t, os.WriteFile(single, []byte("data"), 0o600))

	infoA, err := os.Lstat(a)
	require.NoError(t, err)
	_, _, shared := SharedFileID(a, infoA)
	assert.False(t, shared, "a file with one link is counted directly")

	linkOrSkip(t, a, b)

	infoA, err = os.Lstat(a)
	require.NoError(t, err)
	infoB, err := os.Lstat(b)
	require.NoError(t, err)
	infoS, err := os.Lstat(single)
	require.NoError(t, err)

	idA, nA, okA := SharedFileID(a, infoA)
	idB, _, okB := SharedFileID(b, infoB)
	require.True(t, okA)
	require.True(t, okB)
	assert.Equal(t, idA, idB, "both links name the same file")
	assert.Equal(t, uint64(2), nA)

	_, _, okS := SharedFileID(single, infoS)
	assert.False(t, okS)
}

func TestLinkSetCountsEachIDOnce(t *testing.T) {
	var s LinkSet
	id := FileID{Volume: 1, Index: 2}

	assert.True(t, s.Add(id))
	assert.False(t, s.Add(id))
	assert.True(t, s.Add(FileID{Volume: 1, Index: 3}))
}
