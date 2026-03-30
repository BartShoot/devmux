//go:build cgo && ghostty

package tui

import (
	"sync"

	"devmux/internal/terminal"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TerminalView is a tview primitive that renders a terminal emulator
type TerminalView struct {
	*tview.Box
	term       *terminal.Terminal
	name       string
	mu         sync.Mutex
	focused    bool
	autoScroll bool
}

// NewTerminalView creates a new terminal view with the given name
func NewTerminalView(name string) *TerminalView {
	tv := &TerminalView{
		Box:        tview.NewBox(),
		name:       name,
		autoScroll: true,
	}
	tv.SetBorder(true)
	tv.SetTitle(" " + name + " ")

	// Initialize terminal with default size (will be resized on first draw)
	term, err := terminal.New(80, 24)
	if err == nil {
		tv.term = term
	}

	return tv
}

// SetTerminal sets the terminal emulator for this view
func (tv *TerminalView) SetTerminal(term *terminal.Terminal) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.term = term
}

// GetTerminal returns the terminal emulator
func (tv *TerminalView) GetTerminal() *terminal.Terminal {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return tv.term
}

// Draw renders the terminal content
func (tv *TerminalView) Draw(screen tcell.Screen) {
	tv.Box.DrawForSubclass(screen, tv)

	tv.mu.Lock()
	defer tv.mu.Unlock()

	if tv.term == nil {
		return
	}

	// Get the inner area (excluding border)
	x, y, width, height := tv.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	// Resize terminal if needed
	cols, rows := tv.term.Size()
	if cols != width || rows != height {
		tv.term.Resize(width, height)
	}

	// Get screen content from terminal
	cells := tv.term.GetScreen()
	cursor := tv.term.GetCursor()

	// Default colors
	defStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)

	// Render each cell
	for row := 0; row < len(cells) && row < height; row++ {
		for col := 0; col < len(cells[row]) && col < width; col++ {
			cell := cells[row][col]

			style := defStyle

			// Apply foreground color
			if !cell.FG.Default {
				style = style.Foreground(tcell.NewRGBColor(int32(cell.FG.R), int32(cell.FG.G), int32(cell.FG.B)))
			}

			// Apply background color
			if !cell.BG.Default {
				style = style.Background(tcell.NewRGBColor(int32(cell.BG.R), int32(cell.BG.G), int32(cell.BG.B)))
			}

			// Apply text attributes
			if cell.Bold {
				style = style.Bold(true)
			}
			if cell.Italic {
				style = style.Italic(true)
			}
			if cell.Underline {
				style = style.Underline(true)
			}
			if cell.Strikethrough {
				style = style.StrikeThrough(true)
			}

			char := cell.Char
			if char == 0 {
				char = ' '
			}

			screen.SetContent(x+col, y+row, char, nil, style)
		}
	}

	// Draw cursor if visible and focused
	if tv.focused && cursor.Visible && cursor.X < width && cursor.Y < height {
		cursorX := x + cursor.X
		cursorY := y + cursor.Y

		// Get current cell content at cursor position
		mainc, combc, style, _ := screen.GetContent(cursorX, cursorY)

		// Invert colors for cursor
		fg, bg, attrs := style.Decompose()
		cursorStyle := style.Foreground(bg).Background(fg).Attributes(attrs)

		screen.SetContent(cursorX, cursorY, mainc, combc, cursorStyle)
	}
}

// Focus is called when this primitive receives focus
func (tv *TerminalView) Focus(delegate func(p tview.Primitive)) {
	tv.mu.Lock()
	tv.focused = true
	tv.mu.Unlock()
	tv.Box.Focus(delegate)
}

// Blur is called when this primitive loses focus
func (tv *TerminalView) Blur() {
	tv.mu.Lock()
	tv.focused = false
	tv.mu.Unlock()
	tv.Box.Blur()
}

// HasFocus returns whether this primitive has focus
func (tv *TerminalView) HasFocus() bool {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return tv.focused
}

// Write feeds data to the terminal emulator
func (tv *TerminalView) Write(data []byte) error {
	tv.mu.Lock()
	term := tv.term
	tv.mu.Unlock()

	if term == nil {
		return nil
	}
	return term.Write(data)
}

// SetAutoScroll sets whether to auto-scroll on new content
func (tv *TerminalView) SetAutoScroll(auto bool) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.autoScroll = auto
}

// GetAutoScroll returns whether auto-scroll is enabled
func (tv *TerminalView) GetAutoScroll() bool {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return tv.autoScroll
}

// InputHandler returns the handler for this primitive
func (tv *TerminalView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return tv.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		// Input handling is done at the TUI level to send to daemon
	})
}

// MouseHandler returns the mouse handler for this primitive
func (tv *TerminalView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return tv.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		if action == tview.MouseLeftClick {
			setFocus(tv)
			return true, nil
		}
		return false, nil
	})
}
