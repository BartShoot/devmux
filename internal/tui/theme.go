package tui

import "github.com/gdamore/tcell/v2"

// Catppuccin Mocha palette for TUI chrome.
// Pane content defers to terminal defaults.
var (
	colBase     = tcell.NewRGBColor(30, 30, 46)   // #1e1e2e
	colMantle   = tcell.NewRGBColor(24, 24, 37)   // #181825
	colSurface0 = tcell.NewRGBColor(49, 50, 68)   // #313244
	colSurface1 = tcell.NewRGBColor(69, 71, 90)   // #45475a
	colText     = tcell.NewRGBColor(205, 214, 244) // #cdd6f4
	colSubtext0 = tcell.NewRGBColor(166, 173, 200) // #a6adc8
	colOverlay0 = tcell.NewRGBColor(108, 112, 134) // #6c7086
	colLavender = tcell.NewRGBColor(180, 190, 254) // #b4befe
	colGreen    = tcell.NewRGBColor(166, 227, 161) // #a6e3a1
	colYellow   = tcell.NewRGBColor(249, 226, 175) // #f9e2af
	colRed      = tcell.NewRGBColor(243, 139, 168) // #f38ba8
	colPeach    = tcell.NewRGBColor(250, 179, 135) // #fab387
	colBlue     = tcell.NewRGBColor(137, 180, 250) // #89b4fa
)
