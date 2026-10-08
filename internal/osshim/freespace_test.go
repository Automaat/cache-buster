package osshim

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryDiskSpace(t *testing.T) {
	space, err := QueryDiskSpace(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, space.Free)
	assert.GreaterOrEqual(t, space.Total, space.Free)
}

func TestQueryDiskSpaceMissingPathFails(t *testing.T) {
	_, err := QueryDiskSpace(filepath.Join(t.TempDir(), "missing", "deeper"))
	assert.Error(t, err)
}
