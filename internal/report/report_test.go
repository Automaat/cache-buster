package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entries(sizes ...int64) []provider.Entry {
	out := make([]provider.Entry, len(sizes))
	for i, s := range sizes {
		out[i] = provider.Entry{Path: fmt.Sprintf("/p/%d", i), Size: s}
	}
	return out
}

func TestLargest(t *testing.T) {
	in := entries(5, 9, 1, 9, 7, 3, 8, 2)
	snapshot := append([]provider.Entry(nil), in...)

	got := Largest(in, 5)
	require.Len(t, got, 5)

	var sizes []int64
	for _, e := range got {
		sizes = append(sizes, e.Size)
	}
	assert.Equal(t, []int64{9, 9, 8, 7, 5}, sizes)
	assert.Equal(t, "/p/1", got[0].Path, "equal sizes order by path")
	assert.Equal(t, snapshot, in, "input is not reordered")
	assert.Nil(t, Largest(nil, 5))
	assert.Len(t, Largest(entries(1, 2), 5), 2)
}

func TestWriteBlock(t *testing.T) {
	tests := []struct {
		name  string
		block Block
		want  string
	}{
		{
			name: "dry-run with entries",
			block: Block{Name: "npm", Status: StatusDryRun,
				Summary: Summarize(StatusDryRun, 30, entries(10, 20, 30, 40, 50, 60, 70), 2)},
			want: "npm: would free 30 B (7 entries, 2 skipped)\n" +
				"  largest:\n" +
				"          70 B  /p/6\n" +
				"          60 B  /p/5\n" +
				"          50 B  /p/4\n" +
				"          40 B  /p/3\n" +
				"          30 B  /p/2\n" +
				"    ... and 2 more (--verbose lists all)\n",
		},
		{
			name:  "dry-run command without entries shows its note",
			block: Block{Name: "go", Status: StatusDryRun, Output: "would run: go clean -cache", Summary: Summarize(StatusDryRun, 0, nil, 0)},
			want:  "go: would free 0 B\n  would run: go clean -cache\n",
		},
		{
			name:  "cleaned has no note",
			block: Block{Name: "go", Status: StatusCleaned, Output: "noise", Summary: Summarize(StatusCleaned, 2048, nil, 0)},
			want:  "go: freed 2.0 KiB\n",
		},
		{
			name:  "error",
			block: Block{Name: "docker", Status: "error", Err: "boom"},
			want:  "docker: error: boom\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			WriteBlock(&buf, tt.block)
			assert.Equal(t, tt.want, buf.String())
		})
	}
}

func TestSkippedAndTotal(t *testing.T) {
	blocks := []Block{
		{Name: "a", Status: StatusDryRun, Summary: Summarize(StatusDryRun, 100, entries(60, 40), 0)},
		{Name: "b", Status: StatusSkipped, Reason: "within limit"},
		{Name: "c", Status: "error", Err: "x"},
	}
	var buf bytes.Buffer
	WriteSkipped(&buf, blocks[1:2])
	WriteTotal(&buf, Totals(blocks, true))

	assert.Equal(t, "skipped (1):\n  b: within limit\n"+
		"total: would free 100 B across 1 provider, 2 entries; 1 skipped; 1 failed\n", buf.String())
}

func TestWriteBlockTruncatesNotesOnRuneBoundary(t *testing.T) {
	note := strings.Repeat("é", 200)
	var buf bytes.Buffer
	WriteBlock(&buf, Block{Name: "x", Status: StatusDryRun, Output: note, Summary: Summarize(StatusDryRun, 0, nil, 0)})
	assert.True(t, utf8.ValidString(buf.String()))
	assert.Contains(t, buf.String(), strings.Repeat("é", maxNoteWidth)+"...")
}
