//go:build unix

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlocksToBytes(t *testing.T) {
	got, err := blocksToBytes(10, uint32(4096))
	require.NoError(t, err)
	assert.Equal(t, uint64(40960), got)

	got, err = blocksToBytes(3, int64(512))
	require.NoError(t, err)
	assert.Equal(t, uint64(1536), got)

	_, err = blocksToBytes(3, int64(0))
	assert.Error(t, err)
	_, err = blocksToBytes(3, int64(-1))
	assert.Error(t, err)
}
