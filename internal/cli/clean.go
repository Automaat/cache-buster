package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/smykla-skalski/bilgie/internal/config"
	"github.com/smykla-skalski/bilgie/internal/provider"
	"github.com/smykla-skalski/bilgie/internal/report"
	"github.com/smykla-skalski/bilgie/pkg/size"
	"github.com/spf13/cobra"
)

// CleanCmd cleans caches to free disk space.
var CleanCmd = &cobra.Command{
	Use:   "clean [providers...]",
	Short: "Clean caches to free disk space",
	Long: `Clean caches for specified providers or all enabled providers with --all flag.

By default, runs full clean using native tool commands (e.g., 'go clean -cache').
Use --smart for LRU-based cleaning that removes old files until cache reaches max_size.`,
	RunE: runClean,
}

func init() {
	CleanCmd.Flags().Bool("all", false, "Clean all enabled providers")
	CleanCmd.Flags().Bool("dry-run", false, "Preview without deleting")
	CleanCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	CleanCmd.Flags().Bool("quiet", false, "Minimal output")
	CleanCmd.Flags().Bool("json", false, "Output results in JSON format (requires --force or --dry-run)")
	CleanCmd.Flags().Bool("verbose", false, "List every entry instead of a per-provider summary")
	CleanCmd.Flags().Bool("smart", false, "Smart clean: removes files older than max_age, then LRU-trims to stay under max_size")
}

func runClean(cmd *cobra.Command, args []string) error {
	allFlag, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	quiet, _ := cmd.Flags().GetBool("quiet")
	smart, _ := cmd.Flags().GetBool("smart")
	jsonOut, _ := cmd.Flags().GetBool("json")
	verbose, _ := cmd.Flags().GetBool("verbose")

	return runCleanWithOptions(config.NewLoader(), args, cleanOptions{
		all: allFlag, dryRun: dryRun, force: force, quiet: quiet, smart: smart, json: jsonOut, verbose: verbose,
	}, os.Stdin)
}

type cleanOptions struct {
	all, dryRun, force, quiet, smart, json, verbose bool
}

func runCleanWithLoader(loader *config.Loader, args []string, allFlag, dryRun, force, quiet, smart bool, stdin *os.File) error {
	return runCleanWithOptions(loader, args, cleanOptions{
		all: allFlag, dryRun: dryRun, force: force, quiet: quiet, smart: smart,
	}, stdin)
}

func runCleanWithOptions(loader *config.Loader, args []string, opts cleanOptions, stdin *os.File) error {
	return runCleanWithContext(interruptContext, loader, args, opts, stdin)
}

func interruptContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runCleanWithContext runs clean under the context from newCtx, created only
// after the confirmation prompt so Ctrl-C still aborts the prompt.
func runCleanWithContext(newCtx func() (context.Context, context.CancelFunc), loader *config.Loader, args []string, opts cleanOptions, stdin *os.File) error {
	allFlag, dryRun, force, quiet, smart := opts.all, opts.dryRun, opts.force, opts.quiet, opts.smart
	if opts.json && !force && !dryRun {
		return fmt.Errorf("--json requires --force or --dry-run (no interactive prompt)")
	}

	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providerNames, err := resolveProviders(cfg, args, allFlag, smart)
	if err != nil {
		return err
	}

	providers, unavailable := loadAndFilterProviders(cfg, providerNames)
	if len(providers) == 0 {
		if opts.json {
			results := make([]ProviderCleanResult, 0, len(unavailable))
			for _, u := range unavailable {
				results = append(results, ProviderCleanResult{Name: u.Name, Status: statusUnavailable, Reason: u.Reason})
			}
			_ = finishClean(results, 0, opts, nil, false)
		}
		if len(unavailable) > 0 {
			details := make([]string, len(unavailable))
			for i, u := range unavailable {
				details[i] = u.String()
			}
			return fmt.Errorf("no available providers to clean: %s", strings.Join(details, ", "))
		}
		return fmt.Errorf("no available providers to clean")
	}

	for _, u := range unavailable {
		if quiet || opts.json {
			continue
		}
		if report.IsLoadFailure(u.Name, u.Reason) {
			fmt.Fprintln(os.Stderr, u.Reason)
			continue
		}
		fmt.Fprintf(os.Stderr, "Skipping %s: %s\n", u.Name, u.Reason)
	}

	if !force && !dryRun {
		if !confirmClean(providers, smart, stdin) {
			fmt.Println("Aborted")
			return nil
		}
	}

	ctx, stop := newCtx()
	defer stop()

	mode := provider.CleanModeFull
	if smart {
		mode = provider.CleanModeSmart
	}

	return executeClean(ctx, providers, unavailable, opts, mode)
}

// volumesProvider is skipped by --all --smart: smart mode cannot bound a
// volume prune, so it must be requested by name.
const volumesProvider = "docker-volumes"

func resolveProviders(cfg *config.Config, args []string, allFlag, smart bool) ([]string, error) {
	if len(args) == 0 && !allFlag {
		available := cfg.EnabledProviders()
		return nil, fmt.Errorf("specify providers or use --all\nAvailable: %s", strings.Join(available, ", "))
	}

	if allFlag {
		names := cfg.EnabledProviders()
		if !smart {
			return names, nil
		}
		return slices.DeleteFunc(names, func(n string) bool { return n == volumesProvider }), nil
	}

	enabled := make(map[string]bool)
	for _, name := range cfg.EnabledProviders() {
		enabled[name] = true
	}

	var invalid []string
	for _, name := range args {
		if !enabled[name] {
			invalid = append(invalid, name)
		}
	}

	if len(invalid) > 0 {
		available := cfg.EnabledProviders()
		return nil, fmt.Errorf("unknown providers: %s\nAvailable: %s",
			strings.Join(invalid, ", "), strings.Join(available, ", "))
	}

	return args, nil
}

// unavailableProvider is a provider that could not be loaded or is not installed.
type unavailableProvider struct {
	Name   string
	Reason string
}

func (u unavailableProvider) String() string {
	if report.IsLoadFailure(u.Name, u.Reason) {
		return u.Reason
	}
	if u.Reason == "" {
		return u.Name
	}
	return fmt.Sprintf("%s (%s)", u.Name, u.Reason)
}

func loadAndFilterProviders(cfg *config.Config, names []string) (providers []provider.Provider, unavailable []unavailableProvider) {
	for _, name := range names {
		p, err := provider.LoadProvider(name, cfg)
		if err != nil {
			unavailable = append(unavailable, unavailableProvider{Name: name, Reason: err.Error()})
			continue
		}

		if !p.Available() {
			unavailable = append(unavailable, unavailableProvider{Name: name, Reason: "unavailable"})
			continue
		}

		providers = append(providers, p)
	}

	return providers, unavailable
}

func confirmClean(providers []provider.Provider, smart bool, stdin *os.File) bool {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}

	modeStr := "full"
	if smart {
		modeStr = "smart"
	}
	fmt.Printf("Clean %d provider(s) [%s]: %s? [y/N]: ", len(providers), modeStr, strings.Join(names, ", "))

	reader := bufio.NewReader(stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	return response == "y" || response == "yes"
}

// ProviderCleanResult is one provider's outcome in clean output.
type ProviderCleanResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
	Output     string `json:"output,omitempty"`
	Freed      string `json:"freed,omitempty"`
	FreedBytes int64  `json:"freed_bytes"`
	// Summary is the structured outcome; entries are not parsed from Output.
	Summary *report.Summary `json:"summary,omitempty"`
}

// CleanOutput holds the full clean outcome for JSON serialization.
type CleanOutput struct {
	Total      string                `json:"total"`
	Providers  []ProviderCleanResult `json:"providers"`
	Summary    report.Overall        `json:"summary"`
	TotalBytes int64                 `json:"total_bytes"`
	DryRun     bool                  `json:"dry_run"`
	Cancelled  bool                  `json:"cancelled,omitempty"`
}

// Statuses reported per provider.
const (
	statusCleaned     = "cleaned"
	statusDryRun      = "dry-run"
	statusSkipped     = "skipped"
	statusError       = "error"
	statusUnavailable = "unavailable"
	statusCancelled   = "cancelled"
)

func executeClean(
	ctx context.Context,
	providers []provider.Provider,
	unavailable []unavailableProvider,
	opts cleanOptions,
	mode provider.CleanMode,
) error {
	dryRun, quiet, jsonOut := opts.dryRun, opts.quiet, opts.json
	text := !jsonOut
	concise := !opts.verbose
	skips := skipList{enabled: text && concise && !quiet, unavailable: unavailable}
	var totalCleaned int64
	var errors []string
	results := make([]ProviderCleanResult, 0, len(providers)+len(unavailable))

	for _, u := range unavailable {
		results = append(results, ProviderCleanResult{Name: u.Name, Status: statusUnavailable, Reason: u.Reason})
	}

	for _, p := range providers {
		select {
		case <-ctx.Done():
			if !quiet && text {
				fmt.Println("\nCancelled")
			}
			skips.flush()
			return finishClean(results, totalCleaned, opts, errors, true)
		default:
		}

		if !quiet && !dryRun && text {
			fmt.Printf("Cleaning %s... ", p.Name())
		}

		result, err := p.Clean(ctx, provider.CleanOptions{DryRun: dryRun, Mode: mode})
		totalCleaned += result.BytesCleaned
		if err != nil && ctx.Err() != nil {
			// An interrupt killed the command; report it like any other cancel.
			results = append(results, ProviderCleanResult{
				Name:       p.Name(),
				Status:     statusCancelled,
				Output:     strings.TrimSpace(result.Output),
				Freed:      size.FormatSize(result.BytesCleaned),
				FreedBytes: result.BytesCleaned,
				Summary:    summaryOf(statusCancelled, result),
			})
			if !quiet && text {
				fmt.Println("error")
				fmt.Println("\nCancelled")
			}
			skips.flush()
			return finishClean(results, totalCleaned, opts, errors, true)
		}
		entry := ProviderCleanResult{
			Name:       p.Name(),
			Output:     strings.TrimSpace(result.Output),
			Freed:      size.FormatSize(result.BytesCleaned),
			FreedBytes: result.BytesCleaned,
		}

		switch {
		case err != nil:
			entry.Status = statusError
			entry.Error = err.Error()
			errors = append(errors, fmt.Sprintf("%s: %v", p.Name(), err))
			if !quiet && text {
				fmt.Println("error")
				if result.Output != "" {
					fmt.Print(result.Output)
				}
			}
		case result.SkipReason != "":
			entry.Status = statusSkipped
			entry.Reason = result.SkipReason
			skips.add(p.Name(), result.SkipReason)
			if text && (!skips.enabled || !dryRun) {
				printSkipped(p.Name(), result.SkipReason, dryRun, quiet)
			}
		case dryRun:
			entry.Status = statusDryRun
			switch {
			case !quiet && text && concise:
				report.WriteBlock(os.Stdout, blockOf(p.Name(), statusDryRun, result))
			case !quiet && text:
				fmt.Printf("[dry-run] %s: %s\n", p.Name(), result.Output)
			}
		default:
			entry.Status = statusCleaned
			if !quiet && text {
				printDone(result, concise)
			}
		}
		entry.Summary = summaryOf(entry.Status, result)
		results = append(results, entry)
	}

	skips.flush()
	return finishClean(results, totalCleaned, opts, errors, false)
}

// skipList gathers the providers that did not run for the closing
// "skipped (N)" section of concise text output.
type skipList struct {
	blocks      []report.Block
	unavailable []unavailableProvider
	enabled     bool
}

func (l *skipList) add(name, reason string) {
	if l.enabled {
		l.blocks = append(l.blocks, report.Block{Name: name, Status: statusSkipped, Reason: reason})
	}
}

func (l *skipList) flush() {
	if !l.enabled {
		return
	}
	for _, u := range l.unavailable {
		l.blocks = append(l.blocks, report.Block{Name: u.Name, Status: statusUnavailable, Reason: u.Reason})
	}
	report.WriteSkipped(os.Stdout, l.blocks)
	l.blocks = nil
	l.unavailable = nil
}

func printDone(result provider.CleanResult, concise bool) {
	defer report.WriteWarnings(os.Stdout, result.Warnings)
	if !concise {
		fmt.Printf("done (freed %s)\n", size.FormatSize(result.BytesCleaned))
		if out := strings.TrimSpace(result.Output); out != "" {
			fmt.Println(out)
		}
		return
	}
	fmt.Printf("done (freed %s%s)\n", size.FormatSize(result.BytesCleaned), entryCount(result))
	report.WriteTop(os.Stdout, report.Summarize(statusCleaned, result.BytesCleaned, result.Entries, 0))
}

func summaryOf(status string, result provider.CleanResult) *report.Summary {
	s := report.Summarize(status, result.BytesCleaned, result.Entries, result.SkippedEntries)
	s.Warnings = result.Warnings
	return &s
}

func blockOf(name, status string, result provider.CleanResult) report.Block {
	return report.Block{
		Name:    name,
		Status:  status,
		Reason:  result.SkipReason,
		Output:  strings.TrimSpace(result.Output),
		Summary: *summaryOf(status, result),
	}
}

// entryCount is the ", N entries" suffix of a finished provider line.
func entryCount(result provider.CleanResult) string {
	switch n := len(result.Entries); n {
	case 0:
		return ""
	case 1:
		return ", 1 entry"
	default:
		return fmt.Sprintf(", %d entries", n)
	}
}

// printSkipped reports a skipped provider. Skips go to stderr in quiet mode so
// they stay visible without breaking the single-line byte count on stdout.
func printSkipped(name, reason string, dryRun, quiet bool) {
	switch {
	case quiet:
		fmt.Fprintf(os.Stderr, "skipped %s: %s\n", name, reason)
	case dryRun:
		fmt.Printf("[dry-run] %s: skipped (%s)\n", name, reason)
	default:
		fmt.Printf("skipped (%s)\n", reason)
	}
}

func finishClean(results []ProviderCleanResult, totalCleaned int64, opts cleanOptions, errors []string, cancelled bool) error {
	switch {
	case opts.json:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(CleanOutput{
			Providers:  results,
			Summary:    overallOf(results, opts.dryRun),
			TotalBytes: totalCleaned,
			Total:      size.FormatSize(totalCleaned),
			DryRun:     opts.dryRun,
			Cancelled:  cancelled,
		}); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	case !opts.quiet && opts.dryRun && !opts.verbose:
		report.WriteTotal(os.Stdout, overallOf(results, true))
		if cancelled {
			return nil
		}
	case cancelled:
		return nil
	case !opts.quiet && !opts.dryRun:
		fmt.Printf("\nTotal: %s freed\n", size.FormatSize(totalCleaned))
	case opts.quiet:
		fmt.Println(size.FormatSize(totalCleaned))
	}

	if len(errors) > 0 {
		return fmt.Errorf("some providers failed:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}

// overallOf folds provider results into the run summary for JSON and the
// closing total line.
func overallOf(results []ProviderCleanResult, dryRun bool) report.Overall {
	blocks := make([]report.Block, len(results))
	for i, r := range results {
		blocks[i] = report.Block{Status: r.Status, Err: r.Error}
		if r.Summary != nil {
			blocks[i].Summary = *r.Summary
		}
	}
	return report.Totals(blocks, dryRun)
}
