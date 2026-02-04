package cli

import "github.com/charmbracelet/lipgloss"

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("93"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	overStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	totalStyle  = lipgloss.NewStyle().Bold(true)
)
