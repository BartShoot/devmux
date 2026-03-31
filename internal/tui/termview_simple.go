package tui

import (
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
	cells     []protocol.CellData
	cols      int // terminal cols from daemon
	rows      int // terminal rows from daemon
	viewCols  int // last sent view width
	viewRows  int // last sent view height
	cursor    protocol.CursorData
	selection *protocol.SelectionMsg
	focused   bool
	mu        sync.RWMutex
}

// NewSimpleTerminalView creates a new simple terminal view
func NewSimpleTerminalView(paneID protocol.PaneID, name string) *SimpleTerminalView {
	tv := &SimpleTerminalView{
		Box:    tview.NewBox(),
		paneID: paneID,
		name:   name,
		cols:   80,
		rows:   24,
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
			// Get current cell content at cursor position
			mainc, combc, style, _ := screen.GetContent(cursorX, cursorY)

			// Invert colors for cursor
			fg, bg, attrs := style.Decompose()
			cursorStyle := style.Foreground(bg).Background(fg).Attributes(attrs)

			screen.SetContent(cursorX, cursorY, mainc, combc, cursorStyle)
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
		// Focus on click
		if action == tview.MouseLeftClick {
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
