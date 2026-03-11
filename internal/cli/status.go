package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/internal/provider"
	"github.com/Automaat/cache-buster/pkg/size"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// ProviderStatus holds scan result for a single provider.
type ProviderStatus struct {
	Name           string `json:"name"`
	CurrentFmt     string `json:"current"`
	MaxFmt         string `json:"max"`
	Error          string `json:"error,omitempty"`
	DiskImageFmt   string `json:"disk_image,omitempty"`
	Current        int64  `json:"current_bytes"`
	Max            int64  `json:"max_bytes"`
	DiskImageBytes int64  `json:"disk_image_bytes,omitempty"`
	OverLimit      bool   `json:"over_limit"`
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
	cfg, err := loader.Load()
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
	status := ProviderStatus{Name: name}

	p, err := provider.LoadProvider(name, cfg)
	if err != nil {
		status.Error = fmt.Sprintf("load provider: %v", err)
		return status
	}

	status.Max = p.MaxSize()
	status.MaxFmt = size.FormatSize(p.MaxSize())

	current, err := p.CurrentSize()
	if err != nil {
		status.Error = fmt.Sprintf("get current size: %v", err)
		return status
	}

	status.Current = current
	status.CurrentFmt = size.FormatSize(current)
	status.OverLimit = current > p.MaxSize()

	if ds, ok := p.(provider.DiskSizer); ok {
		if diskSize, diskErr := ds.DiskImageSize(); diskErr == nil && diskSize > 0 && diskSize != current {
			status.DiskImageBytes = diskSize
			status.DiskImageFmt = size.FormatSize(diskSize)
		}
	}

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
		if s.DiskImageFmt != "" {
			currentFmt = fmt.Sprintf("%s (%s on disk)", s.CurrentFmt, s.DiskImageFmt)
		}
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

	width, _, _ := term.GetSize(os.Stdout.Fd())
	if width <= 0 {
		width = 80
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(dimStyle).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		}).
		Headers("Provider", "Current", "Max", "Status").
		Rows(rows...).
		Width(width)

	fmt.Println(t)
	fmt.Println()
	fmt.Println(totalStyle.Render(fmt.Sprintf("Total: %s", size.FormatSize(total))))

	return nil
}
