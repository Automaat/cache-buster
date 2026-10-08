package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/Automaat/cache-buster/internal/auto"
	"github.com/Automaat/cache-buster/pkg/size"
	"github.com/spf13/cobra"
)

// defaultHistoryLimit is how many runs history shows without -n.
const defaultHistoryLimit = 10

// HistoryCmd lists recent auto runs.
var HistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show recent auto runs",
	Long: `Shows the newest runs recorded by auto in ~/.local/state/cache-buster/runs.jsonl:
tier, free space before and after, bytes freed and what was skipped. Unreadable log lines are skipped.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runHistory,
}

func init() {
	HistoryCmd.Flags().IntP("limit", "n", defaultHistoryLimit, "Number of runs to show (0 shows all)")
	HistoryCmd.Flags().Bool("json", false, "Output in JSON format")
}

func runHistory(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	stateDir, err := auto.StateDir()
	if err != nil {
		return err
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
	fmt.Fprintln(w, "TIME\tTIER\tFREE\tFREED\tCLEANED\tSKIPPED\tERRORS\tNOTES")
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
		fmt.Fprintf(w, "%s\t%s\t%s -> %s\t%s\t%d\t%d\t%d\t%s\n",
			r.Time.Local().Format("2006-01-02 15:04"), r.Tier,
			size.FormatSize(r.FreeBefore), size.FormatSize(r.FreeAfter),
			size.FormatSize(r.FreedBytes), cleaned, skipped, failed, historyNotes(r))
	}
	_ = w.Flush()
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
