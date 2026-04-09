package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"localsync/localsync/internal/model"
	"localsync/localsync/internal/service"
)

const maxLogs = 500

// ServerInfo holds static server information for display.
type ServerInfo struct {
	File      string
	Quality   string
	Version   string
	Port      int
	StreamURL string
	WsURL     string
	Variants  []model.Variant
}

// HostModel is the Bubble Tea model for the server TUI.
type HostModel struct {
	hub      *service.Hub
	info     ServerInfo
	logs     []string
	showLogs bool
	newLogs  int
	width    int
	height   int
	logScroll int
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// NewHostModel creates a new host TUI model.
func NewHostModel(hub *service.Hub, info ServerInfo) HostModel {
	return HostModel{
		hub:  hub,
		info: info,
	}
}

func (m HostModel) Init() tea.Cmd {
	return tickCmd()
}

func (m HostModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "l", "L":
			m.showLogs = !m.showLogs
			if m.showLogs {
				m.newLogs = 0
				m.logScroll = max(0, len(m.logs)-m.logPanelHeight())
			}
			return m, nil
		case "up", "k":
			if m.showLogs && m.logScroll > 0 {
				m.logScroll--
			}
			return m, nil
		case "down", "j":
			if m.showLogs {
				maxScroll := max(0, len(m.logs)-m.logPanelHeight())
				if m.logScroll < maxScroll {
					m.logScroll++
				}
			}
			return m, nil
		}

	case tickMsg:
		return m, tickCmd()

	case LogMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		if !m.showLogs {
			m.newLogs++
		} else {
			// Auto-scroll if at bottom
			maxScroll := max(0, len(m.logs)-m.logPanelHeight())
			if m.logScroll >= maxScroll-1 {
				m.logScroll = maxScroll
			}
		}
		return m, nil
	}

	return m, nil
}

func (m HostModel) View() tea.View {
	if m.width == 0 {
		return tea.NewView("Initializing...")
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf(" LocalSync %s  |  %s  |  quality: %s  |  :%d",
		m.info.Version, m.info.File, m.info.Quality, m.info.Port)
	b.WriteString(headerStyle.Width(m.width - 2).Render(header))
	b.WriteString("\n")

	// URLs
	b.WriteString(dimStyle.Render(fmt.Sprintf("  Stream: %s", m.info.StreamURL)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  WS:     %s", m.info.WsURL)))
	b.WriteString("\n\n")

	// Client stats
	stats := m.hub.GetClientStats()
	cutoff := time.Now().Add(-5 * time.Second)

	var activeStats []model.ClientInfo
	for _, s := range stats {
		if !s.LastUpdate.IsZero() && s.LastUpdate.After(cutoff) {
			activeStats = append(activeStats, s)
		}
	}

	if len(activeStats) == 0 {
		b.WriteString(dimStyle.Render("  Waiting for client to connect..."))
		b.WriteString("\n")
	} else {
		// Table header
		b.WriteString(tableHeaderStyle.Render(fmt.Sprintf("  %-12s %-22s %10s %10s %10s %10s",
			"Name", "IP", "Speed", "Buffer", "Size", "Position")))
		b.WriteString("\n")

		for _, s := range activeStats {
			name := s.Name
			if name == "" {
				name = "unknown"
			}

			speed := speedStyle(s.SpeedKbps).Render(fmt.Sprintf("%6.0f kbps", s.SpeedKbps))
			buffer := formatBuffer(s.BufferSecs)
			size := formatBytes(s.BufferBytes)
			pos := formatTime(s.Pos)

			b.WriteString(fmt.Sprintf("  %-12s %-22s %s %10s %10s %10s",
				name, s.IP, speed, buffer, size, pos))
			b.WriteString("\n")
		}
	}

	// Calculate remaining space
	usedLines := strings.Count(b.String(), "\n") + 2 // +2 for status bar + padding

	// Log panel
	if m.showLogs {
		logHeight := m.logPanelHeight()
		if logHeight > 0 {
			b.WriteString("\n")
			logTitle := dimStyle.Render("  Logs ")
			b.WriteString(logTitle)
			b.WriteString("\n")

			start := m.logScroll
			end := start + logHeight
			if end > len(m.logs) {
				end = len(m.logs)
			}
			if start > len(m.logs) {
				start = len(m.logs)
			}

			for i := start; i < end; i++ {
				line := m.logs[i]
				if len(line) > m.width-4 {
					line = line[:m.width-4]
				}
				b.WriteString(dimStyle.Render("  " + line))
				b.WriteString("\n")
			}

			// Pad remaining lines
			rendered := end - start
			for i := rendered; i < logHeight; i++ {
				b.WriteString("\n")
			}
		}
	} else {
		// Fill remaining space
		remaining := m.height - usedLines
		for i := 0; i < remaining; i++ {
			b.WriteString("\n")
		}
	}

	// Status bar
	logsKey := keyStyle.Render("[L]") + " Logs"
	if m.newLogs > 0 {
		logsKey += " " + badgeStyle.Render(fmt.Sprintf("(%d new)", m.newLogs))
	}
	if m.showLogs {
		logsKey = keyStyle.Render("[L]") + " Hide Logs  " +
			dimStyle.Render("[j/k]") + " Scroll"
	}
	quitKey := keyStyle.Render("[Q]") + " Quit"
	statusBar := fmt.Sprintf(" %s  |  %s", logsKey, quitKey)
	b.WriteString(statusBarStyle.Width(m.width).Render(statusBar))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m HostModel) logPanelHeight() int {
	// Reserve lines for: header(1) + urls(2) + blank(1) + stats header(1) + at least 1 stat row + blank(1) + log title(1) + status bar(1)
	reserved := 10
	available := m.height - reserved
	if available < 3 {
		return 3
	}
	half := m.height / 2
	if available > half {
		return half
	}
	return available
}

func formatTime(secs float64) string {
	if secs <= 0 {
		return "00:00"
	}
	m := int(secs) / 60
	s := int(secs) % 60
	if m >= 60 {
		h := m / 60
		m = m % 60
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatBuffer(secs float64) string {
	if secs <= 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444")).Render("0.0s")
	}
	if secs < 3 {
		return warnStyle.Render(fmt.Sprintf("%.1fs", secs))
	}
	return goodStyle.Render(fmt.Sprintf("%.1fs", secs))
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const mb = 1024 * 1024
	const kb = 1024
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
