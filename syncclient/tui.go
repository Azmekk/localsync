package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// --- Styles ---

var (
	tuiHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(0, 1)

	tuiDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))

	tuiKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa"))

	tuiGoodStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00d4aa"))

	tuiWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffaa00"))

	tuiErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4444"))

	tuiStatusBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Background(lipgloss.Color("#1a1a2e"))

	tuiBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff4444"))

	tuiSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00d4aa"))

	tuiCursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00d4aa"))
)

// --- Variant Selector TUI ---

type selectorModel struct {
	items    []selectorItem
	cursor   int
	selected *Variant
	done     bool
}

type selectorItem struct {
	name    string
	desc    string
	variant *Variant // nil = source
}

func newSelectorModel(initMsg InitMessage) selectorModel {
	items := []selectorItem{
		{name: "source", desc: initMsg.File},
	}
	for i := range initMsg.Variants {
		v := &initMsg.Variants[i]
		sizeMB := float64(v.Size) / (1024 * 1024)
		items = append(items, selectorItem{
			name:    v.Name,
			desc:    fmt.Sprintf("%.0f MB", sizeMB),
			variant: v,
		})
	}
	return selectorModel{items: items}
}

func (m selectorModel) Init() tea.Cmd { return nil }

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.items[m.cursor].variant
			m.done = true
			return m, tea.Quit
		case "escape", "q":
			m.done = true
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectorModel) View() tea.View {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(tuiHeaderStyle.Render(" Select Video Variant "))
	b.WriteString("\n\n")

	for i, item := range m.items {
		cursor := "  "
		nameStyle := tuiDimStyle
		if i == m.cursor {
			cursor = tuiCursorStyle.Render("> ")
			nameStyle = tuiSelectedStyle
		}
		b.WriteString(fmt.Sprintf("  %s%s  %s\n",
			cursor,
			nameStyle.Render(item.name),
			tuiDimStyle.Render(item.desc),
		))
	}

	b.WriteString("\n")
	b.WriteString(tuiDimStyle.Render("  [j/k] navigate  [enter] select  [esc] source"))
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// SelectVariant runs the variant selector TUI and returns the selected variant.
// Returns nil for source.
func SelectVariant(initMsg InitMessage) (*Variant, error) {
	m := newSelectorModel(initMsg)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}
	result := finalModel.(selectorModel)
	return result.selected, nil
}

// --- Playback TUI ---

type logMsg string

type tuiLogWriter struct {
	program *tea.Program
}

func (w *tuiLogWriter) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	for _, line := range lines {
		if line != "" {
			w.program.Send(logMsg(line))
		}
	}
	return len(p), nil
}

// statsUpdate is sent from the IPC/WS goroutines to update the TUI.
type statsUpdate struct {
	Pos         float64
	BufferSecs  float64
	BufferBytes int64
	SpeedKbps   float64
	Paused      bool
}

type syncEventMsg string

type playbackTickMsg time.Time

type playbackModel struct {
	file        string
	host        string
	variantName string

	pos         float64
	bufferSecs  float64
	bufferBytes int64
	speedKbps   float64
	paused      bool

	logs      []string
	showLogs  bool
	newLogs   int
	logScroll int
	width     int
	height    int

	statusMsg  string
	statusTime time.Time
}

func newPlaybackModel(file, host, variantName string) playbackModel {
	return playbackModel{
		file:        file,
		host:        host,
		variantName: variantName,
	}
}

func playbackTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return playbackTickMsg(t)
	})
}

func (m playbackModel) Init() tea.Cmd {
	return playbackTickCmd()
}

func (m playbackModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

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
		case "up", "k":
			if m.showLogs && m.logScroll > 0 {
				m.logScroll--
			}
		case "down", "j":
			if m.showLogs {
				maxScroll := max(0, len(m.logs)-m.logPanelHeight())
				if m.logScroll < maxScroll {
					m.logScroll++
				}
			}
		}

	case playbackTickMsg:
		// Clear stale status messages
		if m.statusMsg != "" && time.Since(m.statusTime) > 3*time.Second {
			m.statusMsg = ""
		}
		return m, playbackTickCmd()

	case statsUpdate:
		m.pos = msg.Pos
		m.bufferSecs = msg.BufferSecs
		m.bufferBytes = msg.BufferBytes
		m.speedKbps = msg.SpeedKbps
		m.paused = msg.Paused

	case syncEventMsg:
		m.statusMsg = string(msg)
		m.statusTime = time.Now()

	case logMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > 500 {
			m.logs = m.logs[len(m.logs)-500:]
		}
		if !m.showLogs {
			m.newLogs++
		} else {
			maxScroll := max(0, len(m.logs)-m.logPanelHeight())
			if m.logScroll >= maxScroll-1 {
				m.logScroll = maxScroll
			}
		}
	}

	return m, nil
}

func (m playbackModel) View() tea.View {
	if m.width == 0 {
		return tea.NewView("Initializing...")
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf(" syncclient  |  %s  |  %s", m.host, m.file)
	if m.variantName != "" {
		header += fmt.Sprintf("  |  variant: %s", m.variantName)
	}
	b.WriteString(tuiHeaderStyle.Width(m.width - 2).Render(header))
	b.WriteString("\n\n")

	// Status panel
	playState := tuiGoodStyle.Render("Playing")
	if m.paused {
		playState = tuiWarnStyle.Render("Paused")
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n", lipgloss.NewStyle().Bold(true).Render("Status:"), playState))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		lipgloss.NewStyle().Bold(true).Render("Position:"),
		formatPlaybackTime(m.pos)))
	b.WriteString(fmt.Sprintf("  %s  %s  |  %s\n",
		lipgloss.NewStyle().Bold(true).Render("Buffer:"),
		formatPlaybackBuffer(m.bufferSecs),
		formatPlaybackBytes(m.bufferBytes)))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		lipgloss.NewStyle().Bold(true).Render("Speed:"),
		formatPlaybackSpeed(m.speedKbps)))

	// Sync event
	if m.statusMsg != "" {
		b.WriteString("\n")
		b.WriteString(tuiWarnStyle.Render("  " + m.statusMsg))
		b.WriteString("\n")
	}

	// Log panel
	if m.showLogs {
		logHeight := m.logPanelHeight()
		if logHeight > 0 {
			b.WriteString("\n")
			b.WriteString(tuiDimStyle.Render("  Logs"))
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
				b.WriteString(tuiDimStyle.Render("  " + line))
				b.WriteString("\n")
			}
		}
	}

	// Pad to bottom
	currentLines := strings.Count(b.String(), "\n") + 2
	for i := currentLines; i < m.height-1; i++ {
		b.WriteString("\n")
	}

	// Status bar
	logsKey := tuiKeyStyle.Render("[L]") + " Logs"
	if m.newLogs > 0 {
		logsKey += " " + tuiBadgeStyle.Render(fmt.Sprintf("(%d new)", m.newLogs))
	}
	if m.showLogs {
		logsKey = tuiKeyStyle.Render("[L]") + " Hide Logs  " +
			tuiDimStyle.Render("[j/k]") + " Scroll"
	}
	quitKey := tuiKeyStyle.Render("[Q]") + " Quit"
	statusBar := fmt.Sprintf(" %s  |  %s", logsKey, quitKey)
	b.WriteString(tuiStatusBarStyle.Width(m.width).Render(statusBar))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m playbackModel) logPanelHeight() int {
	reserved := 12
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

func formatPlaybackTime(secs float64) string {
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

func formatPlaybackBuffer(secs float64) string {
	if secs <= 0 {
		return tuiErrorStyle.Render("0.0s")
	}
	if secs < 3 {
		return tuiWarnStyle.Render(fmt.Sprintf("%.1fs", secs))
	}
	return tuiGoodStyle.Render(fmt.Sprintf("%.1fs", secs))
}

func formatPlaybackBytes(bytes int64) string {
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

func formatPlaybackSpeed(kbps float64) string {
	switch {
	case kbps >= 3000:
		return tuiGoodStyle.Render(fmt.Sprintf("%.0f kbps", kbps))
	case kbps >= 1000:
		return tuiWarnStyle.Render(fmt.Sprintf("%.0f kbps", kbps))
	default:
		if kbps > 0 {
			return tuiErrorStyle.Render(fmt.Sprintf("%.0f kbps", kbps))
		}
		return tuiDimStyle.Render("-- kbps")
	}
}
