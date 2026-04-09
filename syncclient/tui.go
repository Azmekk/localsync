package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// --- Log Writer ---

type tuiLogWriter struct {
	view *tview.TextView
	app  *tview.Application
}

func (w *tuiLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	fmtText := text + "\n"
	go w.app.QueueUpdateDraw(func() {
		w.view.Write([]byte(fmtText))
	})
	return len(p), nil
}

// --- Variant Selector ---

// SelectVariant shows a tview list for variant selection. Returns nil for source.
func SelectVariant(initMsg InitMessage) (*Variant, error) {
	app := tview.NewApplication()
	var selected *Variant

	list := tview.NewList().
		ShowSecondaryText(true).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkCyan)

	// Add source option
	list.AddItem("source", initMsg.File, '1', func() {
		selected = nil
		app.Stop()
	})

	// Add variants
	for i := range initMsg.Variants {
		v := &initMsg.Variants[i]
		sizeMB := float64(v.Size) / (1024 * 1024)
		variant := v // capture
		list.AddItem(v.Name, fmt.Sprintf("%.0f MB", sizeMB), rune('2'+i), func() {
			selected = variant
			app.Stop()
		})
	}

	list.SetTitle(" Select Video Variant ").
		SetTitleColor(tcell.ColorDarkCyan).
		SetBorder(true).
		SetBorderColor(tcell.ColorDimGray).
		SetBorderPadding(1, 1, 2, 2)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			selected = nil
			app.Stop()
			return nil
		}
		return event
	})

	app.SetRoot(list, true)
	if err := app.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

// --- Playback TUI ---

type statsUpdate struct {
	Pos         float64
	BufferSecs  float64
	BufferBytes int64
	SpeedKbps   float64
	Paused      bool
}

type syncEventMsg string

// PlaybackTUI holds the tview components for the client playback screen.
type PlaybackTUI struct {
	App       *tview.Application
	LogWriter *tuiLogWriter
	logView   *tview.TextView
	statusView *tview.TextView
	pages     *tview.Pages
	showLogs  bool
	statusBar *tview.TextView
}

func NewPlaybackTUI(file, host, variantName string) *PlaybackTUI {
	app := tview.NewApplication()

	// Header
	headerText := fmt.Sprintf("[::b][#00d4aa]syncclient[-]  |  %s  |  %s", host, file)
	if variantName != "" {
		headerText += fmt.Sprintf("  |  variant: %s", variantName)
	}
	header := tview.NewTextView().
		SetDynamicColors(true).
		SetText(headerText)
	header.SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	// Status panel
	statusView := tview.NewTextView().
		SetDynamicColors(true).
		SetText("  [::b]Status:[-]   Connecting...\n  [::b]Position:[-] 00:00\n  [::b]Buffer:[-]   0.0s  |  0 B\n  [::b]Speed:[-]    -- kbps")
	statusView.SetTitle(" Playback ").SetTitleColor(tcell.ColorDarkCyan).SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	// Log view
	logView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		ScrollToEnd().
		SetMaxLines(500)
	logView.SetTitle(" Logs ").SetTitleColor(tcell.ColorDarkCyan).SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	logWriter := &tuiLogWriter{view: logView, app: app}

	// Status bar
	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [#00d4aa::b][L][-::-] Logs  |  [#00d4aa::b][Q][-::-] Quit")

	// Layouts
	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 3, 0, false).
		AddItem(statusView, 6, 0, false).
		AddItem(tview.NewBox(), 0, 1, false). // spacer
		AddItem(statusBar, 1, 0, false)

	logsLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 3, 0, false).
		AddItem(statusView, 6, 0, false).
		AddItem(logView, 0, 1, false).
		AddItem(statusBar, 1, 0, false)

	pages := tview.NewPages().
		AddPage("main", mainLayout, true, true).
		AddPage("logs", logsLayout, true, false)

	pt := &PlaybackTUI{
		App:        app,
		LogWriter:  logWriter,
		logView:    logView,
		statusView: statusView,
		pages:      pages,
		statusBar:  statusBar,
	}

	// Key handler
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q', 'Q':
			app.Stop()
			return nil
		case 'l', 'L':
			pt.showLogs = !pt.showLogs
			if pt.showLogs {
				pages.SwitchToPage("logs")
				app.SetFocus(logView)
			} else {
				pages.SwitchToPage("main")
			}
			pt.updateStatusBar()
			return nil
		}
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return event
	})

	app.SetRoot(pages, true)
	return pt
}

func (pt *PlaybackTUI) updateStatusBar() {
	if pt.showLogs {
		pt.statusBar.SetText(" [#00d4aa::b][L][-::-] Hide Logs  |  [#00d4aa::b][Q][-::-] Quit")
	} else {
		pt.statusBar.SetText(" [#00d4aa::b][L][-::-] Logs  |  [#00d4aa::b][Q][-::-] Quit")
	}
}

// UpdateStats updates the playback status display.
func (pt *PlaybackTUI) UpdateStats(s statsUpdate) {
	go pt.App.QueueUpdateDraw(func() {
		playState := "[#00d4aa]Playing[-]"
		if s.Paused {
			playState = "[yellow]Paused[-]"
		}

		speedText := "[#555555]-- kbps[-]"
		if s.SpeedKbps > 0 {
			color := "#00d4aa"
			if s.SpeedKbps < 1000 {
				color = "#ff4444"
			} else if s.SpeedKbps < 3000 {
				color = "yellow"
			}
			speedText = fmt.Sprintf("[%s]%.0f kbps[-]", color, s.SpeedKbps)
		}

		bufColor := "#00d4aa"
		if s.BufferSecs < 3 {
			bufColor = "yellow"
		}
		if s.BufferSecs <= 0 {
			bufColor = "#ff4444"
		}

		pt.statusView.SetText(fmt.Sprintf(
			"  [::b]Status:[-]   %s\n  [::b]Position:[-] %s\n  [::b]Buffer:[-]   [%s]%.1fs[-]  |  %s\n  [::b]Speed:[-]    %s",
			playState,
			formatPlaybackTime(s.Pos),
			bufColor, s.BufferSecs,
			formatPlaybackBytes(s.BufferBytes),
			speedText,
		))
	})
}

// ShowSyncEvent shows a transient sync event message in the logs.
func (pt *PlaybackTUI) ShowSyncEvent(msg string) {
	go pt.App.QueueUpdateDraw(func() {
		pt.logView.Write([]byte("[yellow]" + msg + "[-]\n"))
	})
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
