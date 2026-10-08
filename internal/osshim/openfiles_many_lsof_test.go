//go:build !linux && !windows

package osshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeLsofName(t *testing.T) {
	assert.Equal(t, "/p/Proj_\xc3\x84/Target/f", decodeLsofName(`/p/Proj_\xc3\x84/Target/f`))
	assert.Equal(t, "/p/a\nb", decodeLsofName(`/p/a\nb`))
	assert.Equal(t, "/p/a\x01b", decodeLsofName(`/p/a^Ab`))
	assert.Equal(t, `/p/a\xZZ`, decodeLsofName(`/p/a\xZZ`))
	assert.Equal(t, `/p/a^b`, decodeLsofName(`/p/a^b`))
}

func TestLsofUnderIgnoresCaseWhereVolumesDo(t *testing.T) {
	assert.True(t, lsofUnder("/Users/X/Proj/target/f", "/Users/X/Proj/target"))
	assert.False(t, lsofUnder("/Users/X/Proj/target2/f", "/Users/X/Proj/target"))
}
