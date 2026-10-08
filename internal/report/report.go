// Package report turns provider results into the concise per-provider
// summaries printed by clean and auto, and the summary objects in JSON output.
package report

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/smykla-skalski/bilgie/pkg/size"
)

// TopN is how many of the largest entries a summary keeps.
const TopN = 5

const (
	maxNoteLines = 3
	maxNoteWidth = 160
)

// Provider statuses shared by clean and auto.
const (
	StatusCleaned = "cleaned"
	StatusDryRun  = "dry-run"
	StatusSkipped = "skipped"
)

// Summary is the structured outcome of one provider: what it did, how many
// entries it touched, how many bytes that is and the largest entries.
type Summary struct {
	Action         string           `json:"action"`
	Top            []provider.Entry `json:"top,omitempty"`
	Entries        int              `json:"entries"`
	SkippedEntries int              `json:"skipped_entries,omitempty"`
	Bytes          int64            `json:"bytes"`
}

// Overall is the summary of a whole run across providers.
type Overall struct {
	Action    string `json:"action"`
	Providers int    `json:"providers"`
	Skipped   int    `json:"skipped"`
	Errors    int    `json:"errors"`
	Entries   int    `json:"entries"`
	Bytes     int64  `json:"bytes"`
}

// Action names what a status means for the data: cleaned data was freed, a
// dry-run only reports what would be.
func Action(status string) string {
	switch status {
	case StatusCleaned:
		return "freed"
	case StatusDryRun:
		return "would free"
	default:
		return status
	}
}

// Summarize builds a provider summary. The input slice is not modified.
func Summarize(status string, bytes int64, entries []provider.Entry, skippedEntries int) Summary {
	return Summary{
		Action:         Action(status),
		Entries:        len(entries),
		SkippedEntries: skippedEntries,
		Bytes:          bytes,
		Top:            Largest(entries, TopN),
	}
}

// Largest returns the n largest entries, biggest first, ties by path. It
// keeps only n candidates so a 50000-entry list costs one pass.
func Largest(entries []provider.Entry, n int) []provider.Entry {
	if n <= 0 || len(entries) == 0 {
		return nil
	}
	less := func(a, b provider.Entry) int {
		return cmp.Or(cmp.Compare(b.Size, a.Size), strings.Compare(a.Path, b.Path))
	}
	top := make([]provider.Entry, 0, n+1)
	for _, e := range entries {
		if len(top) == n && less(e, top[n-1]) >= 0 {
			continue
		}
		i, _ := slices.BinarySearchFunc(top, e, less)
		top = slices.Insert(top, i, e)
		if len(top) > n {
			top = top[:n]
		}
	}
	return top
}

// IsLoadFailure reports whether text is already a "provider <name>: ..."
// line, as a provider load error reads, so renderers do not prefix it again.
func IsLoadFailure(name, text string) bool {
	return strings.HasPrefix(text, "provider "+name+": ")
}

// Block is one provider's outcome as the renderer needs it.
type Block struct {
	Name    string
	Status  string
	Reason  string
	Err     string
	Output  string
	Summary Summary
}

// Totals folds blocks into an overall summary. Skipped providers and errors
// are counted apart from the providers that ran.
func Totals(blocks []Block, dryRun bool) Overall {
	status := StatusCleaned
	if dryRun {
		status = StatusDryRun
	}
	o := Overall{Action: Action(status)}
	for i := range blocks {
		b := &blocks[i]
		switch {
		case b.Err != "" || b.Status == "error":
			o.Errors++
		case b.Status == StatusSkipped || b.Status == "unavailable":
			o.Skipped++
		default:
			o.Providers++
		}
		if b.Status != StatusSkipped && b.Status != "unavailable" {
			o.Entries += b.Summary.Entries
			o.Bytes += b.Summary.Bytes
		}
	}
	return o
}

// WriteTop prints the largest entries of a summary.
func WriteTop(w io.Writer, s Summary) {
	if len(s.Top) == 0 {
		return
	}
	fmt.Fprintln(w, "  largest:")
	for _, e := range s.Top {
		detail := ""
		if e.Detail != "" {
			detail = "  (" + e.Detail + ")"
		}
		fmt.Fprintf(w, "    %10s  %s%s\n", size.FormatSize(e.Size), e.Path, detail)
	}
	if more := s.Entries - len(s.Top); more > 0 {
		fmt.Fprintf(w, "    ... and %d more (--verbose lists all)\n", more)
	}
}

// WriteBlock prints one provider that ran: action, size, entry count and the
// largest entries. Without entries (command-based providers) it falls back to
// the first lines of the provider's own output on a dry-run.
func WriteBlock(w io.Writer, b Block) {
	switch {
	case b.Err != "" && IsLoadFailure(b.Name, b.Err):
		fmt.Fprintln(w, b.Err)
		return
	case b.Err != "":
		fmt.Fprintf(w, "%s: error: %s\n", b.Name, b.Err)
		return
	case IsLoadFailure(b.Name, b.Reason):
		fmt.Fprintln(w, b.Reason)
		return
	case b.Status == StatusSkipped || b.Status == "unavailable":
		fmt.Fprintf(w, "%s: skipped (%s)\n", b.Name, b.Reason)
		return
	}

	s := b.Summary
	line := fmt.Sprintf("%s: %s %s", b.Name, s.Action, size.FormatSize(s.Bytes))
	var extra []string
	if s.Entries > 0 {
		extra = append(extra, plural(s.Entries, "entry", "entries"))
	}
	if s.SkippedEntries > 0 {
		extra = append(extra, fmt.Sprintf("%d skipped", s.SkippedEntries))
	}
	if len(extra) > 0 {
		line += " (" + strings.Join(extra, ", ") + ")"
	}
	fmt.Fprintln(w, line)

	if len(s.Top) > 0 {
		WriteTop(w, s)
		return
	}
	if b.Status == StatusDryRun {
		writeNote(w, b.Output)
	}
}

func writeNote(w io.Writer, output string) {
	shown, total := 0, 0
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "skip:") {
			continue
		}
		total++
		if shown >= maxNoteLines {
			continue
		}
		if r := []rune(line); len(r) > maxNoteWidth {
			line = string(r[:maxNoteWidth]) + "..."
		}
		fmt.Fprintf(w, "  %s\n", line)
		shown++
	}
	if total > shown {
		fmt.Fprintf(w, "  ... and %d more lines (--verbose lists all)\n", total-shown)
	}
}

// WriteSkipped prints the providers that did not run, with their reasons.
func WriteSkipped(w io.Writer, blocks []Block) {
	if len(blocks) == 0 {
		return
	}
	fmt.Fprintf(w, "skipped (%d):\n", len(blocks))
	for i := range blocks {
		b := &blocks[i]
		reason := b.Reason
		if reason == "" {
			reason = b.Status
		}
		if IsLoadFailure(b.Name, reason) {
			fmt.Fprintf(w, "  %s\n", reason)
			continue
		}
		fmt.Fprintf(w, "  %s: %s\n", b.Name, reason)
	}
}

// WriteTotal prints the closing line of a run.
func WriteTotal(w io.Writer, o Overall) {
	line := fmt.Sprintf("total: %s %s across %s", o.Action, size.FormatSize(o.Bytes), plural(o.Providers, "provider", "providers"))
	if o.Entries > 0 {
		line += ", " + plural(o.Entries, "entry", "entries")
	}
	if o.Skipped > 0 {
		line += fmt.Sprintf("; %d skipped", o.Skipped)
	}
	if o.Errors > 0 {
		line += fmt.Sprintf("; %d failed", o.Errors)
	}
	fmt.Fprintln(w, line)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
