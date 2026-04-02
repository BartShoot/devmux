package tui

import (
	"fmt"
	"sync"

	"devmux/internal/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SimpleTerminalView is a tview primitive that renders terminal cells received from daemon
// This is a "dumb renderer" - no terminal emulation, just displays what daemon sends
type SimpleTerminalView struct {
	*tview.Box
	paneID    protocol.PaneID
	name      string
	command   string              // currently active command (for display in overlay)
	commands  []protocol.PaneCommand // available command presets
	cells     []protocol.CellData
	cols      int // terminal cols from daemon
	rows      int // terminal rows from daemon
	viewCols  int // last sent view width
	viewRows  int // last sent view height
	cursor    protocol.CursorData
	scroll    *protocol.ScrollInfo
	selection *protocol.SelectionMsg
	running   bool
	focused   bool
	dragging  bool // mouse press captured
	dragged   bool // actual mouse movement occurred during press
	client    *StreamClient
	mu        sync.RWMutex
}

// NewSimpleTerminalView creates a new simple terminal view
func NewSimpleTerminalView(paneID protocol.PaneID, name string) *SimpleTerminalView {
	tv := &SimpleTerminalView{
		Box:     tview.NewBox(),
		paneID:  paneID,
		name:    name,
		cols:    80,
		rows:    24,
		running: true,
	}
	tv.SetBorder(true)
	tv.SetBackgroundColor(colBase)
	tv.SetBorderColor(colOverlay0)
	tv.SetTitle(" " + name + " ")
	tv.SetTitleColor(colSubtext0)
	return tv
}

// PaneID returns the pane ID
func (tv *SimpleTerminalView) PaneID() protocol.PaneID {
	return tv.paneID
}

// UpdateScreen updates the display with new screen data
func (tv *SimpleTerminalView) UpdateScreen(update *protocol.ScreenUpdate) {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	tv.cols = int(update.Cols)
	tv.rows = int(update.Rows)
	tv.cells = update.Cells
	tv.cursor = update.Cursor
	tv.scroll = update.Scroll
}

// UpdateSelection updates the selection state
func (tv *SimpleTerminalView) UpdateSelection(sel *protocol.SelectionMsg) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.selection = sel
}

// Draw renders the terminal content
func (tv *SimpleTerminalView) Draw(screen tcell.Screen) {
	tv.Box.DrawForSubclass(screen, tv)

	tv.mu.RLock()
	defer tv.mu.RUnlock()

	// Get the inner area (excluding border)
	x, y, width, height := tv.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	// Default style — Catppuccin Mocha base
	defStyle := tcell.StyleDefault.Background(colBase).Foreground(colText)

	// Render cells
	cellIndex := 0
	for row := 0; row < tv.rows && row < height; row++ {
		for col := 0; col < tv.cols && col < width; col++ {
			style := defStyle
			char := ' '

			if cellIndex < len(tv.cells) {
				cell := tv.cells[cellIndex]
				char = cell.Char
				if char == 0 {
					char = ' '
				}

				// Apply foreground color
				if !cell.FG.Default {
					style = style.Foreground(tcell.NewRGBColor(int32(cell.FG.R), int32(cell.FG.G), int32(cell.FG.B)))
				}

				// Apply background color
				if !cell.BG.Default {
					style = style.Background(tcell.NewRGBColor(int32(cell.BG.R), int32(cell.BG.G), int32(cell.BG.B)))
				}

				// Apply attributes
				if cell.Attrs&protocol.AttrBold != 0 {
					style = style.Bold(true)
				}
				if cell.Attrs&protocol.AttrItalic != 0 {
					style = style.Italic(true)
				}
				if cell.Attrs&protocol.AttrUnderline != 0 {
					style = style.Underline(true)
				}
				if cell.Attrs&protocol.AttrStrikethrough != 0 {
					style = style.StrikeThrough(true)
				}

				// Check if this cell is in selection
				if tv.selection != nil && tv.selection.Active {
					if tv.isInSelection(col, row) {
						// Invert colors for selection
						fg, bg, attrs := style.Decompose()
						style = style.Foreground(bg).Background(fg).Attributes(attrs)
					}
				}
			}

			screen.SetContent(x+col, y+row, char, nil, style)
			cellIndex++
		}
	}

	// Draw cursor if visible and focused
	if tv.focused && tv.cursor.Visible {
		cursorX := x + int(tv.cursor.X)
		cursorY := y + int(tv.cursor.Y)

		if cursorX >= x && cursorX < x+width && cursorY >= y && cursorY < y+height {
			mainc, combc, style, _ := screen.GetContent(cursorX, cursorY)
			fg, bg, attrs := style.Decompose()
			cursorStyle := style.Foreground(bg).Background(fg).Attributes(attrs)
			screen.SetContent(cursorX, cursorY, mainc, combc, cursorStyle)
		}
	}

	// Scroll indicator (top-right corner when scrolled up)
	if tv.scroll != nil && tv.scroll.Offset+tv.scroll.Len < tv.scroll.Total {
		linesAbove := tv.scroll.Total - tv.scroll.Offset - tv.scroll.Len
		indicator := fmt.Sprintf(" +%d lines ", linesAbove)
		indicatorStyle := tcell.StyleDefault.Background(colSurface0).Foreground(colYellow)
		startCol := width - len(indicator)
		if startCol < 0 {
			startCol = 0
		}
		for i, ch := range indicator {
			if startCol+i < width {
				screen.SetContent(x+startCol+i, y, ch, nil, indicatorStyle)
			}
		}
	}

	// Stopped overlay (bottom of pane when process not running)
	if !tv.running {
		overlayY := y + height - 1
		cmd := tv.command
		if len(cmd) > width-30 {
			cmd = cmd[:width-30] + "..."
		}
		overlayText := fmt.Sprintf(" [Stopped] %s  -- Enter to restart ", cmd)
		if len(overlayText) > width {
			overlayText = overlayText[:width]
		}
		overlayStyle := tcell.StyleDefault.Background(colRed).Foreground(colBase)
		startCol := 0
		for i, ch := range overlayText {
			if startCol+i < width {
				screen.SetContent(x+startCol+i, overlayY, ch, nil, overlayStyle)
			}
		}
		// Fill rest of overlay line
		for i := len([]rune(overlayText)); i < width; i++ {
			screen.SetContent(x+i, overlayY, ' ', nil, overlayStyle)
		}
	}
}

// isInSelection checks if a cell is within the selection
func (tv *SimpleTerminalView) isInSelection(col, row int) bool {
	if tv.selection == nil || !tv.selection.Active {
		return false
	}

	startX, startY := int(tv.selection.StartX), int(tv.selection.StartY)
	endX, endY := int(tv.selection.EndX), int(tv.selection.EndY)

	// Normalize selection bounds
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	// Check if cell is in selection
	if row < startY || row > endY {
		return false
	}
	if row == startY && row == endY {
		return col >= startX && col <= endX
	}
	if row == startY {
		return col >= startX
	}
	if row == endY {
		return col <= endX
	}
	return true
}

// Focus is called when this primitive receives focus
func (tv *SimpleTerminalView) Focus(delegate func(p tview.Primitive)) {
	tv.mu.Lock()
	tv.focused = true
	tv.mu.Unlock()
	tv.Box.Focus(delegate)
}

// Blur is called when this primitive loses focus
func (tv *SimpleTerminalView) Blur() {
	tv.mu.Lock()
	tv.focused = false
	tv.mu.Unlock()
	tv.Box.Blur()
}

// HasFocus returns whether this primitive has focus
func (tv *SimpleTerminalView) HasFocus() bool {
	tv.mu.RLock()
	defer tv.mu.RUnlock()
	return tv.focused
}

// InputHandler returns the handler for this primitive
func (tv *SimpleTerminalView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return tv.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		// Input is now handled by TUI and forwarded to daemon
		// This handler is kept for compatibility but does nothing
	})
}

// MouseHandler returns the mouse handler for this primitive
func (tv *SimpleTerminalView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return tv.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		sx, sy := event.Position()
		ix, iy, iw, ih := tv.GetInnerRect()

		// Convert screen coords to pane-relative, clamped to pane bounds
		relX := sx - ix
		relY := sy - iy
		if relX < 0 {
			relX = 0
		} else if relX >= iw {
			relX = iw - 1
		}
		if relY < 0 {
			relY = 0
		} else if relY >= ih {
			relY = ih - 1
		}

		switch action {
		case tview.MouseLeftDown:
			if !tv.InRect(sx, sy) {
				return false, nil
			}
			setFocus(tv)
			tv.mu.Lock()
			tv.dragging = true
			tv.dragged = false
			tv.mu.Unlock()
			if tv.client != nil {
				go tv.client.SendMouse(tv.paneID, protocol.MousePress, relX, relY)
			}
			return true, tv // capture subsequent events

		case tview.MouseMove:
			tv.mu.Lock()
			dragging := tv.dragging
			if dragging {
				tv.dragged = true
			}
			tv.mu.Unlock()
			if !dragging {
				return false, nil
			}
			if tv.client != nil {
				go tv.client.SendMouse(tv.paneID, protocol.MouseDrag, relX, relY)
			}
			return true, tv // keep capture

		case tview.MouseLeftUp:
			tv.mu.Lock()
			dragging := tv.dragging
			dragged := tv.dragged
			tv.dragging = false
			tv.dragged = false
			tv.mu.Unlock()
			if !dragging {
				return false, nil
			}
			if dragged {
				if tv.client != nil {
					go tv.client.SendMouse(tv.paneID, protocol.MouseRelease, relX, relY)
				}
			}
			return true, nil // release capture

		case tview.MouseLeftClick:
			setFocus(tv)
			return true, nil
		}

		return false, nil
	})
}

// GetInnerSize returns the inner dimensions (excluding border)
func (tv *SimpleTerminalView) GetInnerSize() (width, height int) {
	_, _, w, h := tv.GetInnerRect()
	return w, h
}
