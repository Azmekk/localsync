package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"localsync/localsync/internal/model"
	"localsync/localsync/internal/service"
)

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

// RunHostTUI starts the tview application for the host.
func RunHostTUI(hub *service.Hub, info ServerInfo) (*tview.Application, *TUILogWriter) {
	app := tview.NewApplication()

	// Header
	header := tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf("[::b][#00d4aa]LocalSync %s[-]  |  %s  |  quality: %s  |  :%d\n[#555555]Stream: %s\nWS:     %s[-]",
			info.Version, info.File, info.Quality, info.Port, info.StreamURL, info.WsURL)).
		SetTextAlign(tview.AlignLeft)
	header.SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	// Stats table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(false, false)
	table.SetTitle(" Clients ").SetTitleColor(colorCyan).SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	// Set table headers
	headers := []string{"Name", "IP", "Speed", "Buffer", "Size", "Position"}
	for i, h := range headers {
		table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorDimGray).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false))
	}

	// Log view
	logView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		ScrollToEnd().
		SetMaxLines(500)
	logView.SetTitle(" Logs [L] toggle ").SetTitleColor(colorCyan).SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	logWriter := &TUILogWriter{View: logView, App: app}

	// Status bar
	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText(" [#00d4aa::b][L][-::-] Logs  |  [#00d4aa::b][Q][-::-] Quit")

	// Layout
	showLogs := false

	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 5, 0, false).
		AddItem(table, 0, 1, false).
		AddItem(statusBar, 1, 0, false)

	logsLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 5, 0, false).
		AddItem(table, 0, 1, false).
		AddItem(logView, 0, 1, false).
		AddItem(statusBar, 1, 0, false)

	pages := tview.NewPages().
		AddPage("main", mainLayout, true, true).
		AddPage("logs", logsLayout, true, false)

	updateStatusBar := func() {
		if showLogs {
			statusBar.SetText(" [#00d4aa::b][L][-::-] Hide Logs  |  [#00d4aa::b][Q][-::-] Quit")
		} else {
			statusBar.SetText(" [#00d4aa::b][L][-::-] Logs  |  [#00d4aa::b][Q][-::-] Quit")
		}
	}

	// Key handler
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q', 'Q':
			app.Stop()
			return nil
		case 'l', 'L':
			showLogs = !showLogs
			if showLogs {
				pages.SwitchToPage("logs")
				app.SetFocus(logView)
			} else {
				pages.SwitchToPage("main")
			}
			updateStatusBar()
			return nil
		}
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return event
	})

	// Periodic stats refresh
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			app.QueueUpdateDraw(func() {
				stats := hub.GetClientStats()
				cutoff := time.Now().Add(-5 * time.Second)

				// Clear old rows (keep header)
				rowCount := table.GetRowCount()
				for r := rowCount - 1; r >= 1; r-- {
					table.RemoveRow(r)
				}

				hasActive := false
				row := 1
				for _, s := range stats {
					if s.LastUpdate.IsZero() || s.LastUpdate.Before(cutoff) {
						continue
					}
					hasActive = true
					name := s.Name
					if name == "" {
						name = "unknown"
					}

					table.SetCell(row, 0, tview.NewTableCell(name).SetTextColor(colorWhite))
					table.SetCell(row, 1, tview.NewTableCell(s.IP).SetTextColor(colorWhite))
					table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%.0f kbps", s.SpeedKbps)).SetTextColor(speedColor(s.SpeedKbps)))
					table.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%.1fs", s.BufferSecs)).SetTextColor(bufferColor(s.BufferSecs)))
					table.SetCell(row, 4, tview.NewTableCell(formatBytes(s.BufferBytes)).SetTextColor(colorWhite))
					table.SetCell(row, 5, tview.NewTableCell(formatTime(s.Pos)).SetTextColor(colorWhite))
					row++
				}

				if !hasActive {
					table.SetCell(1, 0, tview.NewTableCell("Waiting for client to connect...").
						SetTextColor(tcell.ColorDimGray).SetSelectable(false))
					for i := 1; i < 6; i++ {
						table.SetCell(1, i, tview.NewTableCell(""))
					}
				}
			})
		}
	}()

	app.SetRoot(pages, true).EnableMouse(false)
	return app, logWriter
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
