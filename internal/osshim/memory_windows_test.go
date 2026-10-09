//go:build windows

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMemoryOnThisHost(t *testing.T) {
	m, err := ReadMemory(t.Context())
	require.NoError(t, err)
	assert.NotZero(t, m.MemTotal)
	assert.LessOrEqual(t, m.SwapUsed, m.SwapTotal)
}
