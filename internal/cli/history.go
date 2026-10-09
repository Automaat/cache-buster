package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/smykla-skalski/bilgie/internal/auto"
	"github.com/smykla-skalski/bilgie/pkg/size"
	"github.com/spf13/cobra"
)

// defaultHistoryLimit is how many runs history shows without -n.
const defaultHistoryLimit = 10

// HistoryCmd lists recent auto runs.
var HistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show recent auto runs",
	Long: `Shows the newest runs recorded by auto in ~/.local/state/bilgie/runs.jsonl:
tier, free space before and after, bytes freed and what was skipped. Unreadable log lines are skipped.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runHistory,
}

func init() {
	HistoryCmd.Flags().IntP("limit", "n", defaultHistoryLimit, "Number of runs to show (0 shows all)")
	HistoryCmd.Flags().Bool("json", false, "Output in JSON format")
	HistoryCmd.Flags().Bool("providers", false, "Show bytes freed per provider over the last N runs")
}

func runHistory(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	providers, _ := cmd.Flags().GetBool("providers")
	stateDir, err := auto.StateDir()
	if err != nil {
		return err
	}
	if providers {
		return runHistoryProvidersIn(os.Stdout, stateDir, limit, jsonFlag)
	}
	return runHistoryIn(os.Stdout, stateDir, limit, jsonFlag)
}

func runHistoryIn(out io.Writer, stateDir string, limit int, jsonOutput bool) error {
	if limit < 0 {
		return fmt.Errorf("--limit must not be negative, got %d", limit)
	}
	runs, corrupt, err := auto.ReadRuns(stateDir, limit)
	if err != nil {
		return err
	}

	if jsonOutput {
		if runs == nil {
			runs = []auto.RunRecord{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(runs)
	}

	if len(runs) == 0 {
		fmt.Fprintln(out, "No auto runs recorded")
	} else {
		writeHistoryTable(out, runs)
	}
	if corrupt > 0 {
		fmt.Fprintf(out, "%d unreadable log line(s) skipped\n", corrupt)
	}
	return nil
}

func writeHistoryTable(out io.Writer, runs []auto.RunRecord) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tTIER\tFREE\tSWAP\tFREED\tCLEANED\tSKIPPED\tERRORS\tNOTES")
	for _, r := range slices.Backward(runs) {
		var cleaned, skipped, failed int
		for _, p := range r.Providers {
			switch p.Status {
			case auto.StatusSkipped:
				skipped++
			case auto.StatusError:
				failed++
			default:
				cleaned++
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s -> %s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			r.Time.Local().Format("2006-01-02 15:04"), r.Tier,
			size.FormatSize(r.FreeBefore), size.FormatSize(r.FreeAfter), swapCell(r.SwapUsedMiB),
			size.FormatSize(r.FreedBytes), cleaned, skipped, failed, historyNotes(r))
	}
	_ = w.Flush()
}

func swapCell(mib int32) string {
	if mib <= 0 {
		return "-"
	}
	return size.FormatSize(int64(mib) << 20)
}

func historyNotes(r auto.RunRecord) string {
	var notes []string
	if r.DryRun {
		notes = append(notes, "dry-run")
	}
	if r.Recovered {
		notes = append(notes, "recovered")
	}
	if r.Notified {
		notes = append(notes, "notified")
	}
	if r.Error != "" {
		notes = append(notes, "run error")
	}
	return strings.Join(notes, ", ")
}

// ProviderHistory is one provider's totals over a window of runs. Freed
// counts deleting runs only; WouldFree counts what dry-runs reported.
type ProviderHistory struct {
	Name      string `json:"name"`
	Runs      int    `json:"runs"`
	Cleaned   int    `json:"cleaned"`
	Skipped   int    `json:"skipped"`
	Errors    int    `json:"errors"`
	Freed     int64  `json:"freed_bytes"`
	WouldFree int64  `json:"would_free_bytes"`
}

func aggregateProviders(runs []auto.RunRecord) []ProviderHistory {
	byName := make(map[string]*ProviderHistory)
	for _, r := range runs {
		for _, p := range r.Providers {
			h := byName[p.Name]
			if h == nil {
				h = &ProviderHistory{Name: p.Name}
				byName[p.Name] = h
			}
			h.Runs++
			switch p.Status {
			case auto.StatusSkipped:
				h.Skipped++
			case auto.StatusError:
				h.Errors++
			default:
				h.Cleaned++
			}
			if r.DryRun {
				h.WouldFree += p.FreedBytes
			} else {
				h.Freed += p.FreedBytes
			}
		}
	}
	out := make([]ProviderHistory, 0, len(byName))
	for _, h := range byName {
		out = append(out, *h)
	}
	slices.SortFunc(out, func(a, b ProviderHistory) int {
		return cmp.Or(cmp.Compare(b.Freed, a.Freed), cmp.Compare(b.WouldFree, a.WouldFree), strings.Compare(a.Name, b.Name))
	})
	return out
}

func runHistoryProvidersIn(out io.Writer, stateDir string, limit int, jsonOutput bool) error {
	if limit < 0 {
		return fmt.Errorf("--limit must not be negative, got %d", limit)
	}
	runs, corrupt, err := auto.ReadRuns(stateDir, limit)
	if err != nil {
		return err
	}
	stats := aggregateProviders(runs)

	if jsonOutput {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Providers []ProviderHistory `json:"providers"`
			Runs      int               `json:"runs"`
		}{Providers: stats, Runs: len(runs)})
	}

	switch {
	case len(runs) == 0:
		fmt.Fprintln(out, "No auto runs recorded")
	case len(stats) == 0:
		fmt.Fprintf(out, "No provider results in the last %d run(s)\n", len(runs))
	default:
		fmt.Fprintf(out, "Bytes freed per provider over the last %d run(s); dry-runs count as WOULD FREE\n", len(runs))
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PROVIDER\tRUNS\tFREED\tWOULD FREE\tSKIPPED\tERRORS")
		for _, h := range stats {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%d\n", h.Name, h.Runs, size.FormatSize(h.Freed), size.FormatSize(h.WouldFree), h.Skipped, h.Errors)
		}
		_ = w.Flush()
	}
	if corrupt > 0 {
		fmt.Fprintf(out, "%d unreadable log line(s) skipped\n", corrupt)
	}
	return nil
}
