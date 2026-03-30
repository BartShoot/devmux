//go:build !cgo || !ghostty

package terminal

import (
	"sync"
)

// Terminal represents a virtual terminal (stub implementation without libghostty)
type Terminal struct {
	cols   int
	rows   int
	buffer [][]Cell
	cursor CursorState
	mu     sync.Mutex
}

// Cell represents a single cell in the terminal grid
type Cell struct {
	Char          rune
	FG            Color
	BG            Color
	Bold          bool
	Italic        bool
	Underline     bool
	Strikethrough bool
}

// Color represents an RGB color
type Color struct {
	R, G, B uint8
	Default bool // true if using terminal default color
}

// CursorState represents the cursor position and visibility
type CursorState struct {
	X, Y    int
	Visible bool
	Style   CursorStyle
}

// CursorStyle represents cursor appearance
type CursorStyle int

const (
	CursorBlock CursorStyle = iota
	CursorUnderline
	CursorBar
)

// New creates a new Terminal with the given dimensions
// This stub implementation does basic text buffering without VT parsing
func New(cols, rows int) (*Terminal, error) {
	t := &Terminal{
		cols:   cols,
		rows:   rows,
		buffer: make([][]Cell, rows),
		cursor: CursorState{X: 0, Y: 0, Visible: true, Style: CursorBlock},
	}

	for i := range t.buffer {
		t.buffer[i] = make([]Cell, cols)
		for j := range t.buffer[i] {
			t.buffer[i][j] = Cell{
				Char: ' ',
				FG:   Color{Default: true},
				BG:   Color{Default: true},
			}
		}
	}

	return t, nil
}

// Close releases all resources
func (t *Terminal) Close() {
	// Nothing to clean up in stub
}

// Write feeds data into the terminal
// Stub implementation: simple text append without VT sequence parsing
func (t *Terminal) Write(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, b := range data {
		switch b {
		case '\n':
			t.cursor.Y++
			t.cursor.X = 0
			if t.cursor.Y >= t.rows {
				t.scrollUp()
				t.cursor.Y = t.rows - 1
			}
		case '\r':
			t.cursor.X = 0
		case '\b':
			if t.cursor.X > 0 {
				t.cursor.X--
			}
		default:
			if b >= 32 && b < 127 { // printable ASCII
				if t.cursor.X < t.cols && t.cursor.Y < t.rows {
					t.buffer[t.cursor.Y][t.cursor.X].Char = rune(b)
					t.cursor.X++
					if t.cursor.X >= t.cols {
						t.cursor.X = 0
						t.cursor.Y++
						if t.cursor.Y >= t.rows {
							t.scrollUp()
							t.cursor.Y = t.rows - 1
						}
					}
				}
			}
		}
	}

	return nil
}

func (t *Terminal) scrollUp() {
	// Shift all rows up by one
	for i := 0; i < t.rows-1; i++ {
		t.buffer[i] = t.buffer[i+1]
	}
	// Clear last row
	t.buffer[t.rows-1] = make([]Cell, t.cols)
	for j := range t.buffer[t.rows-1] {
		t.buffer[t.rows-1][j] = Cell{
			Char: ' ',
			FG:   Color{Default: true},
			BG:   Color{Default: true},
		}
	}
}

// Resize changes the terminal dimensions
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	newBuffer := make([][]Cell, rows)
	for i := range newBuffer {
		newBuffer[i] = make([]Cell, cols)
		for j := range newBuffer[i] {
			newBuffer[i][j] = Cell{Char: ' ', FG: Color{Default: true}, BG: Color{Default: true}}
		}
	}

	// Copy existing content
	for i := 0; i < min(rows, t.rows); i++ {
		for j := 0; j < min(cols, t.cols); j++ {
			newBuffer[i][j] = t.buffer[i][j]
		}
	}

	t.buffer = newBuffer
	t.cols = cols
	t.rows = rows

	// Clamp cursor
	if t.cursor.X >= cols {
		t.cursor.X = cols - 1
	}
	if t.cursor.Y >= rows {
		t.cursor.Y = rows - 1
	}
}

// GetCursor returns the current cursor state
func (t *Terminal) GetCursor() CursorState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursor
}

// GetScreen returns the current screen content as a 2D grid of cells
func (t *Terminal) GetScreen() [][]Cell {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Return a copy
	screen := make([][]Cell, t.rows)
	for i := range screen {
		screen[i] = make([]Cell, t.cols)
		copy(screen[i], t.buffer[i])
	}
	return screen
}

// Size returns the current terminal dimensions
func (t *Terminal) Size() (cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
