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

	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/Automaat/cache-buster/pkg/size"
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
	CleanCmd.Flags().Bool("smart", false, "Smart clean: removes files older than max_age, then LRU-trims to stay under max_size")
}

func runClean(cmd *cobra.Command, args []string) error {
	allFlag, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	quiet, _ := cmd.Flags().GetBool("quiet")
	smart, _ := cmd.Flags().GetBool("smart")
	jsonOut, _ := cmd.Flags().GetBool("json")

	return runCleanWithOptions(config.NewLoader(), args, cleanOptions{
		all: allFlag, dryRun: dryRun, force: force, quiet: quiet, smart: smart, json: jsonOut,
	}, os.Stdin)
}

type cleanOptions struct {
	all, dryRun, force, quiet, smart, json bool
}

func runCleanWithLoader(loader *config.Loader, args []string, allFlag, dryRun, force, quiet, smart bool, stdin *os.File) error {
	return runCleanWithOptions(loader, args, cleanOptions{
		all: allFlag, dryRun: dryRun, force: force, quiet: quiet, smart: smart,
	}, stdin)
}

func runCleanWithOptions(loader *config.Loader, args []string, opts cleanOptions, stdin *os.File) error {
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
		return fmt.Errorf("no available providers to clean")
	}

	for _, name := range unavailable {
		if !quiet && !opts.json {
			fmt.Fprintf(os.Stderr, "Skipping %s: unavailable\n", name)
		}
	}

	if !force && !dryRun {
		if !confirmClean(providers, smart, stdin) {
			fmt.Println("Aborted")
			return nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

func loadAndFilterProviders(cfg *config.Config, names []string) (providers []provider.Provider, unavailable []string) {
	for _, name := range names {
		p, err := provider.LoadProvider(name, cfg)
		if err != nil {
			unavailable = append(unavailable, fmt.Sprintf("%s (load error: %v)", name, err))
			continue
		}

		if !p.Available() {
			unavailable = append(unavailable, name)
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
}

// CleanOutput holds the full clean outcome for JSON serialization.
type CleanOutput struct {
	Total      string                `json:"total"`
	Providers  []ProviderCleanResult `json:"providers"`
	TotalBytes int64                 `json:"total_bytes"`
	DryRun     bool                  `json:"dry_run"`
}

// Statuses reported per provider.
const (
	statusCleaned     = "cleaned"
	statusDryRun      = "dry-run"
	statusSkipped     = "skipped"
	statusError       = "error"
	statusUnavailable = "unavailable"
)

func executeClean(
	ctx context.Context,
	providers []provider.Provider,
	unavailable []string,
	opts cleanOptions,
	mode provider.CleanMode,
) error {
	dryRun, quiet, jsonOut := opts.dryRun, opts.quiet, opts.json
	text := !jsonOut
	var totalCleaned int64
	var errors []string
	results := make([]ProviderCleanResult, 0, len(providers)+len(unavailable))

	for _, name := range unavailable {
		results = append(results, ProviderCleanResult{Name: name, Status: statusUnavailable})
	}

	for _, p := range providers {
		select {
		case <-ctx.Done():
			if !quiet && text {
				fmt.Println("\nCancelled")
			}
			return finishClean(results, totalCleaned, opts, errors, true)
		default:
		}

		if !quiet && !dryRun && text {
			fmt.Printf("Cleaning %s... ", p.Name())
		}

		result, err := p.Clean(ctx, provider.CleanOptions{DryRun: dryRun, Mode: mode})
		totalCleaned += result.BytesCleaned
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
			if text {
				printSkipped(p.Name(), result.SkipReason, dryRun, quiet)
			}
		case dryRun:
			entry.Status = statusDryRun
			if !quiet && text {
				fmt.Printf("[dry-run] %s: %s\n", p.Name(), result.Output)
			}
		default:
			entry.Status = statusCleaned
			if !quiet && text {
				fmt.Printf("done (freed %s)\n", size.FormatSize(result.BytesCleaned))
			}
		}
		results = append(results, entry)
	}

	return finishClean(results, totalCleaned, opts, errors, false)
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
			TotalBytes: totalCleaned,
			Total:      size.FormatSize(totalCleaned),
			DryRun:     opts.dryRun,
		}); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	case cancelled:
		return nil
	case !opts.quiet && !opts.dryRun:
		fmt.Printf("\nTotal: %s freed\n", size.FormatSize(totalCleaned))
	case opts.quiet && !opts.dryRun:
		fmt.Println(size.FormatSize(totalCleaned))
	}

	if len(errors) > 0 {
		return fmt.Errorf("some providers failed:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}
