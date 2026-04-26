package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type JobStatus int

const (
	StatusPending JobStatus = iota
	StatusActive
	StatusDone
	StatusSkipped
	StatusError
	StatusPreExisting
)

type QueueItem struct {
	Job        Job
	Status     JobStatus
	DurationMs int64
	Progress   Progress
	Err        string
}

type Dashboard struct {
	app       *tview.Application
	header    *tview.TextView
	table     *tview.Table
	logView   *tview.TextView
	statusBar *tview.TextView
	pages     *tview.Pages

	ctrl   *Controller
	queue  []*QueueItem
	mu     sync.Mutex
	preset string

	skipReq       chan struct{}
	quitReq       chan struct{}
	quitConfirmed atomic.Bool
	modalOpen     atomic.Bool
}

func NewDashboard(preset string, queue []*QueueItem, ctrl *Controller) *Dashboard {
	d := &Dashboard{
		ctrl:    ctrl,
		queue:   queue,
		preset:  preset,
		skipReq: make(chan struct{}, 1),
		quitReq: make(chan struct{}, 1),
	}
	d.build()
	return d
}

func (d *Dashboard) build() {
	d.app = tview.NewApplication()

	d.header = tview.NewTextView().SetDynamicColors(true)
	d.header.SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	d.table = tview.NewTable().SetBorders(false).SetSelectable(false, false)
	d.table.SetTitle(" Queue ").SetTitleColor(tcell.ColorDarkCyan).
		SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	d.logView = tview.NewTextView().SetDynamicColors(false).SetScrollable(true).SetMaxLines(2000)
	d.logView.SetTitle(" ffmpeg log ").SetTitleColor(tcell.ColorDarkCyan).
		SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	d.statusBar = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	d.statusBar.SetText("[#888888]p pause/resume   s skip   l logs   q quit[-]")

	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.header, 4, 0, false).
		AddItem(d.table, 0, 1, false).
		AddItem(d.statusBar, 1, 0, false)

	logsLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.header, 4, 0, false).
		AddItem(d.table, 0, 1, false).
		AddItem(d.logView, 0, 1, true).
		AddItem(d.statusBar, 1, 0, false)

	d.pages = tview.NewPages().
		AddPage("main", mainLayout, true, true).
		AddPage("logs", logsLayout, true, false)

	d.app.SetInputCapture(d.handleKey)
	d.app.SetRoot(d.pages, true)
}

type tuiLogWriter struct {
	view *tview.TextView
	app  *tview.Application
}

func (w *tuiLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	out := text + "\n"
	go w.app.QueueUpdateDraw(func() {
		_, _ = w.view.Write([]byte(out))
		w.view.ScrollToEnd()
	})
	return len(p), nil
}

func (d *Dashboard) LogWriter() *tuiLogWriter {
	return &tuiLogWriter{view: d.logView, app: d.app}
}

func (d *Dashboard) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	if d.modalOpen.Load() {
		return ev
	}
	switch ev.Rune() {
	case 'p', 'P':
		go func() {
			if _, err := d.ctrl.Toggle(); err != nil {
				log.Printf("pause/resume failed: %v", err)
			}
			d.app.QueueUpdateDraw(d.refreshHeader)
		}()
		return nil
	case 's', 'S':
		select {
		case d.skipReq <- struct{}{}:
		default:
		}
		d.ctrl.Kill()
		return nil
	case 'q', 'Q':
		d.confirmQuit()
		return nil
	case 'l', 'L':
		name, _ := d.pages.GetFrontPage()
		if name == "main" {
			d.pages.SwitchToPage("logs")
		} else {
			d.pages.SwitchToPage("main")
		}
		return nil
	}
	if ev.Key() == tcell.KeyCtrlC {
		d.confirmQuit()
		return nil
	}
	return ev
}

func (d *Dashboard) confirmQuit() {
	if d.modalOpen.Load() {
		return
	}
	d.modalOpen.Store(true)
	modal := tview.NewModal().
		SetText("Quit batchcompress? The current encode will be discarded.").
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(_ int, label string) {
			d.modalOpen.Store(false)
			if label == "Yes" {
				d.quitConfirmed.Store(true)
				d.ctrl.Kill()
				select {
				case d.quitReq <- struct{}{}:
				default:
				}
				d.app.Stop()
			} else {
				d.pages.RemovePage("modal")
			}
		})
	d.pages.AddPage("modal", modal, false, true)
}

func (d *Dashboard) SkipChan() <-chan struct{} { return d.skipReq }
func (d *Dashboard) QuitChan() <-chan struct{} { return d.quitReq }
func (d *Dashboard) QuitConfirmed() bool       { return d.quitConfirmed.Load() }
func (d *Dashboard) Stop()                     { d.app.Stop() }

func (d *Dashboard) Run(ctx context.Context) error {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.app.QueueUpdateDraw(func() {
					d.refreshHeader()
					d.refreshTable()
				})
			}
		}
	}()
	return d.app.Run()
}

func (d *Dashboard) UpdateProgress(idx int, p Progress) {
	d.mu.Lock()
	if idx >= 0 && idx < len(d.queue) {
		d.queue[idx].Progress = p
	}
	d.mu.Unlock()
}

func (d *Dashboard) SetStatus(idx int, status JobStatus, errMsg string) {
	d.mu.Lock()
	if idx >= 0 && idx < len(d.queue) {
		d.queue[idx].Status = status
		d.queue[idx].Err = errMsg
	}
	d.mu.Unlock()
}

func (d *Dashboard) SetDuration(idx int, dur time.Duration) {
	d.mu.Lock()
	if idx >= 0 && idx < len(d.queue) {
		d.queue[idx].DurationMs = dur.Milliseconds()
	}
	d.mu.Unlock()
}

func (d *Dashboard) refreshHeader() {
	d.mu.Lock()
	done := 0
	total := len(d.queue)
	for _, q := range d.queue {
		if q.Status == StatusDone {
			done++
		}
	}
	d.mu.Unlock()

	pausedTag := ""
	if d.ctrl.Paused() {
		pausedTag = "   [#ffaa00::b]PAUSED[-]"
	}
	d.header.SetText(fmt.Sprintf(
		"[#00d4aa::b]batchcompress[-]   preset: [::b]%s[::-]   progress: %d / %d%s\n"+
			"[#888888]p pause/resume   s skip   l logs   q quit[-]",
		d.preset, done, total, pausedTag))
}

func (d *Dashboard) refreshTable() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.table.Clear()
	headers := []string{"#", "File", "Status", "Progress", "Speed", "ETA"}
	for i, h := range headers {
		d.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorDimGray).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false))
	}

	for i, item := range d.queue {
		row := i + 1
		statusText, statusColor := formatStatus(item.Status)

		fileColor := tcell.ColorWhite
		switch item.Status {
		case StatusActive:
			fileColor = tcell.ColorYellow
		case StatusDone:
			fileColor = tcell.NewRGBColor(0, 212, 170)
		case StatusSkipped, StatusPreExisting:
			fileColor = tcell.ColorDimGray
		case StatusError:
			fileColor = tcell.ColorRed
		}

		d.table.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%d", i+1)).
			SetTextColor(tcell.ColorDimGray))
		d.table.SetCell(row, 1, tview.NewTableCell(filepath.Base(item.Job.Source)).
			SetTextColor(fileColor))
		d.table.SetCell(row, 2, tview.NewTableCell(statusText).
			SetTextColor(statusColor))

		var progStr, speedStr, etaStr string
		switch item.Status {
		case StatusActive:
			if item.DurationMs > 0 && item.Progress.OutTimeMs > 0 {
				pct := float64(item.Progress.OutTimeMs) * 100 / float64(item.DurationMs)
				if pct > 100 {
					pct = 100
				}
				progStr = fmt.Sprintf("%.0f%%", pct)
			} else if item.Progress.OutTimeMs > 0 {
				progStr = formatDuration(time.Duration(item.Progress.OutTimeMs) * time.Millisecond)
			} else {
				progStr = "starting"
			}
			if item.Progress.Speed > 0 {
				speedStr = fmt.Sprintf("%.2fx", item.Progress.Speed)
			}
			if item.DurationMs > 0 && item.Progress.Speed > 0 && item.Progress.OutTimeMs > 0 {
				remainMs := item.DurationMs - item.Progress.OutTimeMs
				if remainMs > 0 {
					etaSec := float64(remainMs) / 1000.0 / item.Progress.Speed
					etaStr = formatDuration(time.Duration(etaSec * float64(time.Second)))
				}
			}
		case StatusDone:
			progStr = "100%"
		case StatusError:
			progStr = item.Err
			if len(progStr) > 60 {
				progStr = progStr[:57] + "..."
			}
		case StatusSkipped:
			progStr = "—"
			if item.Err != "" {
				progStr = item.Err
			}
		case StatusPreExisting:
			progStr = "exists"
		}

		d.table.SetCell(row, 3, tview.NewTableCell(progStr).SetTextColor(tcell.ColorWhite))
		d.table.SetCell(row, 4, tview.NewTableCell(speedStr).SetTextColor(tcell.ColorWhite))
		d.table.SetCell(row, 5, tview.NewTableCell(etaStr).SetTextColor(tcell.ColorWhite))
	}
}

func formatStatus(s JobStatus) (string, tcell.Color) {
	switch s {
	case StatusPending:
		return "pending", tcell.ColorDimGray
	case StatusActive:
		return "encoding", tcell.ColorYellow
	case StatusDone:
		return "done", tcell.NewRGBColor(0, 212, 170)
	case StatusSkipped:
		return "skipped", tcell.ColorDimGray
	case StatusError:
		return "error", tcell.ColorRed
	case StatusPreExisting:
		return "exists", tcell.ColorDimGray
	}
	return "", tcell.ColorWhite
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
