package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

// ProviderStatus holds scan result for a single provider.
type ProviderStatus struct {
	Name       string `json:"name"`
	CurrentFmt string `json:"current"`
	MaxFmt     string `json:"max"`
	Error      string `json:"error,omitempty"`
	Current    int64  `json:"current_bytes"`
	Max        int64  `json:"max_bytes"`
	OverLimit  bool   `json:"over_limit"`
}

// StatusOutput holds full status output for JSON serialization.
type StatusOutput struct {
	Total      string           `json:"total"`
	Providers  []ProviderStatus `json:"providers"`
	TotalBytes int64            `json:"total_bytes"`
}

// StatusCmd shows cache status for all enabled providers.
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cache status for all providers",
	RunE:  runStatus,
}

func init() {
	StatusCmd.Flags().Bool("json", false, "Output in JSON format")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	return runStatusWithLoader(config.NewLoader(), jsonFlag)
}

func runStatusWithLoader(loader *config.Loader, jsonOutput bool) error {
	cfg, _, err := loader.LoadOrCreate()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providers := cfg.EnabledProviders()
	if len(providers) == 0 {
		fmt.Println("No enabled providers")
		return nil
	}

	statuses := scanProviders(cfg, providers)

	if jsonOutput {
		return outputJSON(statuses)
	}
	return outputTable(statuses)
}

func scanProviders(cfg *config.Config, names []string) []ProviderStatus {
	statuses := make([]ProviderStatus, len(names))
	var wg sync.WaitGroup

	for i, name := range names {
		wg.Add(1)
		go func(idx int, provName string) {
			defer wg.Done()
			statuses[idx] = scanProvider(cfg, provName)
		}(i, name)
	}
	wg.Wait()
	return statuses
}

func scanProvider(cfg *config.Config, name string) ProviderStatus {
	prov, _ := cfg.GetProvider(name)
	status := ProviderStatus{Name: name}

	maxBytes, err := size.ParseSize(prov.MaxSize)
	if err != nil {
		status.Error = fmt.Sprintf("parse max_size: %v", err)
		return status
	}
	status.Max = maxBytes
	status.MaxFmt = size.FormatSize(maxBytes)

	paths, err := config.ExpandPaths(prov.Paths)
	if err != nil {
		status.Error = fmt.Sprintf("expand paths: %v", err)
		return status
	}

	current, err := cache.CalculateSize(paths)
	if err != nil {
		status.Error = fmt.Sprintf("calculate size: %v", err)
		return status
	}

	status.Current = current
	status.CurrentFmt = size.FormatSize(current)
	status.OverLimit = current > maxBytes
	return status
}

func outputJSON(statuses []ProviderStatus) error {
	var total int64
	for _, s := range statuses {
		total += s.Current
	}

	out := StatusOutput{
		Providers:  statuses,
		TotalBytes: total,
		Total:      size.FormatSize(total),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func outputTable(statuses []ProviderStatus) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("93"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	overStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	totalStyle := lipgloss.NewStyle().Bold(true)

	rows := make([][]string, 0, len(statuses))
	var total int64

	for _, s := range statuses {
		total += s.Current

		statusText := okStyle.Render("ok")
		if s.Error != "" {
			statusText = errorStyle.Render("error")
		} else if s.OverLimit {
			statusText = overStyle.Render("OVER")
		}

		currentFmt := s.CurrentFmt
		maxFmt := s.MaxFmt
		if s.Error != "" {
			if currentFmt == "" {
				currentFmt = "-"
			}
			if maxFmt == "" {
				maxFmt = "-"
			}
		}
		rows = append(rows, []string{s.Name, currentFmt, maxFmt, statusText})
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("240"))).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		}).
		Headers("Provider", "Current", "Max", "Status").
		Rows(rows...)

	fmt.Println(t)
	fmt.Println()
	fmt.Println(totalStyle.Render(fmt.Sprintf("Total: %s", size.FormatSize(total))))

	return nil
}
