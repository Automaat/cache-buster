package osshim

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreeSpace(t *testing.T) {
	free, err := FreeSpace(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, free)
}

func TestFreeSpaceMissingPathFails(t *testing.T) {
	_, err := FreeSpace(filepath.Join(t.TempDir(), "missing", "deeper"))
	assert.Error(t, err)
}
