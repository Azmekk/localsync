package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// LogMsg is sent to the TUI when a log line is captured.
type LogMsg string

// TUILogWriter implements io.Writer and sends each line to a Bubble Tea program.
type TUILogWriter struct {
	Program *tea.Program
}

func (w *TUILogWriter) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	for _, line := range lines {
		if line != "" {
			w.Program.Send(LogMsg(line))
		}
	}
	return len(p), nil
}
