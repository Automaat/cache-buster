package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int
	}{
		{"equal", "2024.1", "2024.1", 0},
		{"minor less", "2024.1", "2024.2", -1},
		{"minor greater", "2024.2", "2024.1", 1},
		{"double digit minor", "2024.9", "2024.10", -1},
		{"major greater", "2025.1", "2024.9", 1},
		{"missing segment counts as zero", "2024", "2024.1", -1},
		{"missing segment equal", "2024", "2024.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compareVersions(tt.v1, tt.v2))
		})
	}
}

// TestCompareVersions_MalformedSegment verifies a non-numeric segment is not
// silently coerced to 0: two versions differing only in a malformed segment
// must order deterministically rather than compare equal.
func TestCompareVersions_MalformedSegment(t *testing.T) {
	assert.NotEqual(t, 0, compareVersions("2024.x", "2024.y"),
		"distinct non-numeric segments must not collapse to equal")
	assert.Equal(t, -compareVersions("2024.y", "2024.x"),
		compareVersions("2024.x", "2024.y"), "comparison must be antisymmetric")
}

// TestCompareVersions_OutOfRangeSegment verifies a numeric segment too large
// for int falls back to lexical comparison instead of silently becoming 0.
func TestCompareVersions_OutOfRangeSegment(t *testing.T) {
	huge := "99999999999999999999999999"
	assert.NotEqual(t, 0, compareVersions("2024."+huge, "2024.1"),
		"out-of-range segment must not collapse to 0")
}
