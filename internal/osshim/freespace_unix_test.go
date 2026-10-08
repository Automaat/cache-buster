//go:build unix

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlocksToBytes(t *testing.T) {
	got, err := blocksToBytes(uint64(10), uint32(4096))
	require.NoError(t, err)
	assert.Equal(t, uint64(40960), got)

	got, err = blocksToBytes(uint64(3), int64(512))
	require.NoError(t, err)
	assert.Equal(t, uint64(1536), got)

	_, err = blocksToBytes(uint64(3), int64(0))
	assert.Error(t, err)
	_, err = blocksToBytes(uint64(3), int64(-1))
	assert.Error(t, err)

	got, err = blocksToBytes(int64(7), int32(1024))
	require.NoError(t, err)
	assert.Equal(t, uint64(7168), got)

	_, err = blocksToBytes(int64(-1), int32(1024))
	assert.Error(t, err)
}
