package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
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
	Long:  `Clean caches for specified providers or all enabled providers with --all flag.`,
	RunE:  runClean,
}

func init() {
	CleanCmd.Flags().Bool("all", false, "Clean all enabled providers")
	CleanCmd.Flags().Bool("dry-run", false, "Preview without deleting")
	CleanCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	CleanCmd.Flags().Bool("quiet", false, "Minimal output")
}

func runClean(cmd *cobra.Command, args []string) error {
	allFlag, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	quiet, _ := cmd.Flags().GetBool("quiet")

	return runCleanWithLoader(config.NewLoader(), args, allFlag, dryRun, force, quiet, os.Stdin)
}

func runCleanWithLoader(loader *config.Loader, args []string, allFlag, dryRun, force, quiet bool, stdin *os.File) error {
	cfg, _, err := loader.LoadOrCreate()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providerNames, err := resolveProviders(cfg, args, allFlag)
	if err != nil {
		return err
	}

	providers, unavailable := loadAndFilterProviders(cfg, providerNames)
	if len(providers) == 0 {
		return fmt.Errorf("no available providers to clean")
	}

	for _, name := range unavailable {
		if !quiet {
			fmt.Fprintf(os.Stderr, "Skipping %s: unavailable\n", name)
		}
	}

	if !force && !dryRun {
		if !confirmClean(providers, stdin) {
			fmt.Println("Aborted")
			return nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return executeClean(ctx, providers, dryRun, quiet)
}

func resolveProviders(cfg *config.Config, args []string, allFlag bool) ([]string, error) {
	if len(args) == 0 && !allFlag {
		available := cfg.EnabledProviders()
		return nil, fmt.Errorf("specify providers or use --all\nAvailable: %s", strings.Join(available, ", "))
	}

	if allFlag {
		return cfg.EnabledProviders(), nil
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

func loadAndFilterProviders(cfg *config.Config, names []string) ([]provider.Provider, []string) {
	var providers []provider.Provider
	var unavailable []string

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

func confirmClean(providers []provider.Provider, stdin *os.File) bool {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}

	fmt.Printf("Clean %d provider(s): %s? [y/N]: ", len(providers), strings.Join(names, ", "))

	reader := bufio.NewReader(stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	return response == "y" || response == "yes"
}

func executeClean(ctx context.Context, providers []provider.Provider, dryRun, quiet bool) error {
	var totalCleaned int64
	var errors []string

	for _, p := range providers {
		select {
		case <-ctx.Done():
			if !quiet {
				fmt.Println("\nCancelled")
			}
			return nil
		default:
		}

		if !quiet && !dryRun {
			fmt.Printf("Cleaning %s... ", p.Name())
		}

		result, err := p.Clean(ctx, provider.CleanOptions{DryRun: dryRun})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", p.Name(), err))
			if !quiet {
				fmt.Println("error")
			}
			continue
		}

		totalCleaned += result.BytesCleaned

		if dryRun {
			if !quiet {
				fmt.Printf("[dry-run] %s: %s\n", p.Name(), result.Output)
			}
		} else if !quiet {
			fmt.Printf("done (freed %s)\n", size.FormatSize(result.BytesCleaned))
		}
	}

	if !quiet && !dryRun {
		fmt.Printf("\nTotal: %s freed\n", size.FormatSize(totalCleaned))
	} else if quiet && !dryRun {
		fmt.Println(size.FormatSize(totalCleaned))
	}

	if len(errors) > 0 {
		return fmt.Errorf("some providers failed:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}
