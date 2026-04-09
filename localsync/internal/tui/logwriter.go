package tui

import (
	"strings"

	"github.com/rivo/tview"
)

// TUILogWriter implements io.Writer and appends lines to a tview.TextView.
// It uses a non-blocking approach to avoid deadlocking the tview event loop.
type TUILogWriter struct {
	View *tview.TextView
	App  *tview.Application
}

func (w *TUILogWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	// Write directly to the TextView (thread-safe) and trigger a draw
	fmt_text := text + "\n"
	go w.App.QueueUpdateDraw(func() {
		w.View.Write([]byte(fmt_text))
	})
	return len(p), nil
}
