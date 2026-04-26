package main

import (
	"fmt"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type pickerState struct {
	root      string
	recursive bool
	files     []SourceFile
	selected  []bool
}

// RunPicker shows the file-selection screen and returns the chosen paths.
// Returns (nil, false) if the user cancelled.
func RunPicker(root string, initialRecursive bool) ([]string, bool) {
	state := &pickerState{root: root, recursive: initialRecursive}
	if err := state.rescan(); err != nil {
		fmt.Println("scan failed:", err)
		return nil, false
	}

	app := tview.NewApplication()
	list := tview.NewList().ShowSecondaryText(true).SetHighlightFullLine(true)
	list.SetSelectedBackgroundColor(tcell.ColorDarkCyan)
	list.SetTitle(" Files ").SetTitleColor(tcell.ColorDarkCyan).
		SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	header := tview.NewTextView().SetDynamicColors(true)
	header.SetBorder(true).SetBorderColor(tcell.ColorDimGray).SetBorderPadding(0, 0, 1, 1)

	footer := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)

	updateHeader := func() {
		header.SetText(fmt.Sprintf(
			"[#00d4aa::b]batchcompress picker[-]\n"+
				"[#888888]root:[-] %s   [#888888]recursive:[-] %v   [#888888]files:[-] %d   [#888888]selected:[-] %d",
			state.root, state.recursive, len(state.files), state.countSelected()))
	}

	footer.SetText("[#888888]space/enter[-] toggle   [#888888]a[-] all   " +
		"[#888888]r[-] recursive   [#888888]g[-] start   [#888888]q/esc[-] cancel")

	itemLabel := func(i int) string {
		glyph := "[ ]"
		if state.selected[i] {
			glyph = "[x]"
		}
		return fmt.Sprintf("%s %s", glyph, displayPath(state.root, state.files[i].Path))
	}
	itemSecondary := func(i int) string {
		return humanSize(state.files[i].Size)
	}

	rebuild := func() {
		current := list.GetCurrentItem()
		list.Clear()
		for i := range state.files {
			list.AddItem(itemLabel(i), itemSecondary(i), 0, func() {
				state.selected[i] = !state.selected[i]
				list.SetItemText(i, itemLabel(i), itemSecondary(i))
				updateHeader()
			})
		}
		if current >= 0 && current < list.GetItemCount() {
			list.SetCurrentItem(current)
		}
		updateHeader()
	}
	rebuild()

	var result []string
	confirmed := false

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			app.Stop()
			return nil
		}
		switch ev.Rune() {
		case 'q', 'Q':
			app.Stop()
			return nil
		case 'a', 'A':
			selectAll := state.countSelected() < len(state.files)
			for i := range state.selected {
				state.selected[i] = selectAll
			}
			rebuild()
			return nil
		case 'r', 'R':
			state.recursive = !state.recursive
			if err := state.rescan(); err == nil {
				rebuild()
			}
			return nil
		case 'g', 'G':
			confirmed = true
			for i, f := range state.files {
				if state.selected[i] {
					result = append(result, f.Path)
				}
			}
			app.Stop()
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 4, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)

	if err := app.SetRoot(flex, true).Run(); err != nil {
		return nil, false
	}
	if !confirmed {
		return nil, false
	}
	return result, true
}

func (s *pickerState) rescan() error {
	files, err := Scan(s.root, s.recursive)
	if err != nil {
		return err
	}
	s.files = files
	s.selected = make([]bool, len(files))
	return nil
}

func (s *pickerState) countSelected() int {
	n := 0
	for _, v := range s.selected {
		if v {
			n++
		}
	}
	return n
}

func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

func humanSize(n int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(KB))
	}
	return fmt.Sprintf("%d B", n)
}
