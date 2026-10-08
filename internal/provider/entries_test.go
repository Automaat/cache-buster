package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntryProvider_ReportsStructuredEntries(t *testing.T) {
	root := t.TempDir()
	old := makeEntry(t, root, "old", 2048, 48*time.Hour)
	makeEntry(t, root, "fresh", 2048, time.Hour)

	for _, dryRun := range []bool{true, false} {
		res, err := newEntryProvider(t, root, "2K", "").Clean(context.Background(), CleanOptions{DryRun: dryRun})
		require.NoError(t, err)
		require.Len(t, res.Entries, 1)
		assert.Equal(t, Entry{Path: old, Size: 2048}, res.Entries[0])
		if !dryRun {
			break
		}
	}
}
