//go:build cgo && ghostty

package terminal

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/ghostty/include
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/ghostty/lib -lghostty-vt -lutil -lm

#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <ghostty/vt.h>
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// Terminal represents a virtual terminal backed by libghostty
type Terminal struct {
	term        C.GhosttyTerminal
	renderState C.GhosttyRenderState
	rowIter     C.GhosttyRenderStateRowIterator
	rowCells    C.GhosttyRenderStateRowCells
	cols        int
	rows        int
	mu          sync.Mutex
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
func New(cols, rows int) (*Terminal, error) {
	t := &Terminal{
		cols: cols,
		rows: rows,
	}

	// Create terminal with options
	opts := C.GhosttyTerminalOptions{
		cols:           C.uint16_t(cols),
		rows:           C.uint16_t(rows),
		max_scrollback: 10000,
	}

	var err C.GhosttyResult
	err = C.ghostty_terminal_new(nil, &t.term, opts)
	if err != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("failed to create terminal: error code %d", err)
	}

	// Create render state
	err = C.ghostty_render_state_new(nil, &t.renderState)
	if err != C.GHOSTTY_SUCCESS {
		C.ghostty_terminal_free(t.term)
		return nil, fmt.Errorf("failed to create render state: error code %d", err)
	}

	// Create row iterator (reusable)
	err = C.ghostty_render_state_row_iterator_new(nil, &t.rowIter)
	if err != C.GHOSTTY_SUCCESS {
		C.ghostty_render_state_free(t.renderState)
		C.ghostty_terminal_free(t.term)
		return nil, fmt.Errorf("failed to create row iterator: error code %d", err)
	}

	// Create row cells (reusable)
	err = C.ghostty_render_state_row_cells_new(nil, &t.rowCells)
	if err != C.GHOSTTY_SUCCESS {
		C.ghostty_render_state_row_iterator_free(t.rowIter)
		C.ghostty_render_state_free(t.renderState)
		C.ghostty_terminal_free(t.term)
		return nil, fmt.Errorf("failed to create row cells: error code %d", err)
	}

	return t, nil
}

// Close releases all resources
func (t *Terminal) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.rowCells != nil {
		C.ghostty_render_state_row_cells_free(t.rowCells)
		t.rowCells = nil
	}
	if t.rowIter != nil {
		C.ghostty_render_state_row_iterator_free(t.rowIter)
		t.rowIter = nil
	}
	if t.renderState != nil {
		C.ghostty_render_state_free(t.renderState)
		t.renderState = nil
	}
	if t.term != nil {
		C.ghostty_terminal_free(t.term)
		t.term = nil
	}
}

// Write feeds data from the PTY into the terminal emulator
func (t *Terminal) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	C.ghostty_terminal_vt_write(t.term, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
	return nil
}

// Resize changes the terminal dimensions
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cols = cols
	t.rows = rows
	// Cell size in pixels (we use 1:1 since we're doing character-based rendering)
	C.ghostty_terminal_resize(t.term, C.uint16_t(cols), C.uint16_t(rows), 8, 16)
}

// GetCursor returns the current cursor state
func (t *Terminal) GetCursor() CursorState {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Update render state from terminal
	C.ghostty_render_state_update(t.renderState, t.term)

	var visible C.bool
	var hasValue C.bool
	var x, y C.uint16_t
	var style C.GhosttyRenderStateCursorVisualStyle

	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, unsafe.Pointer(&visible))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, unsafe.Pointer(&hasValue))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, unsafe.Pointer(&x))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, unsafe.Pointer(&y))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VISUAL_STYLE, unsafe.Pointer(&style))

	cursorStyle := CursorBlock
	switch style {
	case C.GHOSTTY_RENDER_STATE_CURSOR_VISUAL_STYLE_BAR:
		cursorStyle = CursorBar
	case C.GHOSTTY_RENDER_STATE_CURSOR_VISUAL_STYLE_UNDERLINE:
		cursorStyle = CursorUnderline
	}

	return CursorState{
		X:       int(x),
		Y:       int(y),
		Visible: bool(visible) && bool(hasValue),
		Style:   cursorStyle,
	}
}

// GetScreen returns the current screen content as a 2D grid of cells
func (t *Terminal) GetScreen() [][]Cell {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Update render state
	C.ghostty_render_state_update(t.renderState, t.term)

	screen := make([][]Cell, t.rows)
	for i := range screen {
		screen[i] = make([]Cell, t.cols)
		for j := range screen[i] {
			screen[i][j] = Cell{
				Char: ' ',
				FG:   Color{Default: true},
				BG:   Color{Default: true},
			}
		}
	}

	// Get row iterator from render state
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, unsafe.Pointer(&t.rowIter))

	row := 0
	for C.ghostty_render_state_row_iterator_next(t.rowIter) && row < t.rows {
		// Get cells for this row
		C.ghostty_render_state_row_get(t.rowIter, C.GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, unsafe.Pointer(&t.rowCells))

		col := 0
		for C.ghostty_render_state_row_cells_next(t.rowCells) && col < t.cols {
			// Get grapheme length
			var graphemeLen C.uint32_t
			C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, unsafe.Pointer(&graphemeLen))

			if graphemeLen > 0 {
				// Get grapheme codepoints
				buf := make([]C.uint32_t, graphemeLen)
				C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, unsafe.Pointer(&buf[0]))
				screen[row][col].Char = rune(buf[0])
			}

			// Get foreground color
			var fgColor C.GhosttyColorRgb
			fgResult := C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, unsafe.Pointer(&fgColor))
			if fgResult == C.GHOSTTY_SUCCESS {
				screen[row][col].FG = Color{R: uint8(fgColor.r), G: uint8(fgColor.g), B: uint8(fgColor.b)}
			}

			// Get background color
			var bgColor C.GhosttyColorRgb
			bgResult := C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, unsafe.Pointer(&bgColor))
			if bgResult == C.GHOSTTY_SUCCESS {
				screen[row][col].BG = Color{R: uint8(bgColor.r), G: uint8(bgColor.g), B: uint8(bgColor.b)}
			}

			// Get style
			var cellStyle C.GhosttyStyle
			C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, unsafe.Pointer(&cellStyle))
			screen[row][col].Bold = bool(cellStyle.bold)
			screen[row][col].Italic = bool(cellStyle.italic)
			screen[row][col].Strikethrough = bool(cellStyle.strikethrough)
			// Underline is an enum in ghostty, convert to bool
			screen[row][col].Underline = cellStyle.underline != 0

			col++
		}
		row++
	}

	return screen
}

// Size returns the current terminal dimensions
func (t *Terminal) Size() (cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}
