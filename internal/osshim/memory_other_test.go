//go:build !linux && !windows

package osshim

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunOutputMissingProgram(t *testing.T) {
	_, err := runOutput(t.Context(), "bilgie-no-such-program")
	require.Error(t, err)
}
