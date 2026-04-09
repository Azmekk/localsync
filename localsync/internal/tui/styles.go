package tui

import "charm.land/lipgloss/v2"

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(0, 1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#888888"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Background(lipgloss.Color("#1a1a2e"))

	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa"))

	logPanelBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#333333"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))

	goodStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00d4aa"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffaa00"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4444"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa"))

	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff4444"))
)

func speedStyle(kbps float64) lipgloss.Style {
	switch {
	case kbps >= 3000:
		return goodStyle
	case kbps >= 1000:
		return warnStyle
	default:
		return errorStyle
	}
}
