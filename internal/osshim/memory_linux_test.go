//go:build linux

package osshim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMeminfoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	require.NoError(t, os.WriteFile(path, []byte(linuxMeminfo), 0o600))

	m, err := readMeminfoFile(path)
	require.NoError(t, err)
	assert.Equal(t, uint64(3145728)<<10, m.SwapUsed)

	_, err = readMeminfoFile(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestReadMemoryOnThisHost(t *testing.T) {
	m, err := ReadMemory(t.Context())
	require.NoError(t, err)
	assert.NotZero(t, m.MemTotal)
}
