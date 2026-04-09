package tui

import "github.com/gdamore/tcell/v2"

var (
	colorCyan    = tcell.ColorDarkCyan
	colorGreen   = tcell.NewRGBColor(0, 212, 170)
	colorYellow  = tcell.ColorYellow
	colorRed     = tcell.ColorRed
	colorDim     = tcell.ColorDimGray
	colorWhite   = tcell.ColorWhite
)

func speedColor(kbps float64) tcell.Color {
	switch {
	case kbps >= 3000:
		return colorGreen
	case kbps >= 1000:
		return colorYellow
	default:
		return colorRed
	}
}

func bufferColor(secs float64) tcell.Color {
	if secs < 3 {
		return colorYellow
	}
	return colorGreen
}
