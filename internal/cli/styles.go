package cli

import "github.com/charmbracelet/lipgloss"

// Shared styles for CLI output.
var (
	HeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("93"))
	OkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	OverStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	ErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	DimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	TotalStyle  = lipgloss.NewStyle().Bold(true)
)
