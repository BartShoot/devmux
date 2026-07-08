//go:build cgo && ghostty && !windows

package terminal

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/ghostty/include
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/ghostty/lib/linux -Wl,-rpath,$ORIGIN/../lib -Wl,-rpath,$ORIGIN/lib -lghostty-vt -lutil -lm

#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <ghostty/vt.h>

// C-side helper to construct GhosttyTerminalOptions with correct layout.
// Avoids CGO struct padding mismatches.
static inline GhosttyTerminalOptions make_terminal_opts(uint16_t cols, uint16_t rows, size_t max_scrollback) {
	GhosttyTerminalOptions opts = {0};
	opts.cols = cols;
	opts.rows = rows;
	opts.max_scrollback = max_scrollback;
	return opts;
}

// Flat, padding-free cell used to hand a whole row across the CGO boundary
// in a single call. Reading cells one attribute at a time from Go costs ~6
// CGO crossings per cell; devmux_read_row does the inner loop in C so each
// row costs one crossing instead of cols*6.
typedef struct {
	uint32_t codepoint; // base grapheme codepoint (0 => empty cell)
	uint8_t  fg_r, fg_g, fg_b;
	uint8_t  bg_r, bg_g, bg_b;
	uint8_t  fg_default; // 1 if the cell has no explicit foreground
	uint8_t  bg_default; // 1 if the cell has no explicit background
	uint8_t  flags;      // see DEVMUX_CELL_* below
} DevmuxCell;

enum {
	DEVMUX_CELL_BOLD          = 1 << 0,
	DEVMUX_CELL_ITALIC        = 1 << 1,
	DEVMUX_CELL_UNDERLINE     = 1 << 2,
	DEVMUX_CELL_STRIKETHROUGH = 1 << 3,
};

// devmux_read_row walks the cells of the current row (the iterator must
// already be positioned via GHOSTTY_RENDER_STATE_ROW_DATA_CELLS) and writes
// up to `max` cells into out. Returns the number of cells written.
//
// Colors use the resolved FG_COLOR/BG_COLOR keys (palette indices already
// looked up); a GHOSTTY_INVALID_VALUE result means "use the default color".
// Only the base codepoint is kept, matching the previous Go behavior.
static inline size_t devmux_read_row(GhosttyRenderStateRowCells cells, DevmuxCell* out, size_t max) {
	size_t n = 0;
	while (n < max && ghostty_render_state_row_cells_next(cells)) {
		DevmuxCell* c = &out[n];

		// Base codepoint via the raw cell (avoids needing a grapheme buffer
		// sized to the cluster length).
		GhosttyCell raw = 0;
		c->codepoint = 0;
		if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw) == GHOSTTY_SUCCESS) {
			uint32_t cp = 0;
			if (ghostty_cell_get(raw, GHOSTTY_CELL_DATA_CODEPOINT, &cp) == GHOSTTY_SUCCESS) {
				c->codepoint = cp;
			}
		}

		GhosttyColorRgb fg;
		if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &fg) == GHOSTTY_SUCCESS) {
			c->fg_r = fg.r; c->fg_g = fg.g; c->fg_b = fg.b; c->fg_default = 0;
		} else {
			c->fg_r = c->fg_g = c->fg_b = 0; c->fg_default = 1;
		}

		GhosttyColorRgb bg;
		if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &bg) == GHOSTTY_SUCCESS) {
			c->bg_r = bg.r; c->bg_g = bg.g; c->bg_b = bg.b; c->bg_default = 0;
		} else {
			c->bg_r = c->bg_g = c->bg_b = 0; c->bg_default = 1;
		}

		GhosttyStyle style;
		style.size = sizeof(style);
		uint8_t flags = 0;
		if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style) == GHOSTTY_SUCCESS) {
			if (style.bold)          flags |= DEVMUX_CELL_BOLD;
			if (style.italic)        flags |= DEVMUX_CELL_ITALIC;
			if (style.underline != 0) flags |= DEVMUX_CELL_UNDERLINE;
			if (style.strikethrough) flags |= DEVMUX_CELL_STRIKETHROUGH;
		}
		c->flags = flags;

		n++;
	}
	return n;
}
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// MaxScrollbackBytes is the scrollback buffer size in bytes (not lines).
// Ghostty's page list allocator uses ~1KB per 80-col line, so 10MB ≈ 10,000 lines.
// Increase for more scrollback at the cost of memory per pane.
const MaxScrollbackBytes = 10 * 1024 * 1024 // 10MB

// Terminal represents a virtual terminal backed by libghostty
type Terminal struct {
	term        C.GhosttyTerminal
	renderState C.GhosttyRenderState
	rowIter     C.GhosttyRenderStateRowIterator
	rowCells    C.GhosttyRenderStateRowCells
	cols        int
	rows        int
	graphemeBuf [8]C.uint32_t  // reusable grapheme buffer (avoids per-cell alloc)
	rowBuf      []C.DevmuxCell // reusable per-row buffer for the batched C reader
	mu          sync.Mutex     // guards terminal access (Write, Resize, render_state_update)
	renderMu    sync.Mutex     // guards render-state reads (renderState + iterators + rowBuf)
}

// New creates a new Terminal with the given dimensions
func New(cols, rows int) (*Terminal, error) {
	t := &Terminal{
		cols: cols,
		rows: rows,
	}

	opts := C.make_terminal_opts(C.uint16_t(cols), C.uint16_t(rows), C.size_t(MaxScrollbackBytes))

	var err C.GhosttyResult
	err = C.ghostty_terminal_new(nil, &t.term, opts)
	if err != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("failed to create terminal: error code %d", err)
	}


	err = C.ghostty_render_state_new(nil, &t.renderState)
	if err != C.GHOSTTY_SUCCESS {
		C.ghostty_terminal_free(t.term)
		return nil, fmt.Errorf("failed to create render state: error code %d", err)
	}

	err = C.ghostty_render_state_row_iterator_new(nil, &t.rowIter)
	if err != C.GHOSTTY_SUCCESS {
		C.ghostty_render_state_free(t.renderState)
		C.ghostty_terminal_free(t.term)
		return nil, fmt.Errorf("failed to create row iterator: error code %d", err)
	}

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
	// Lock order matches the readers (renderMu then mu) to avoid freeing the
	// render state out from under an in-flight read.
	t.renderMu.Lock()
	defer t.renderMu.Unlock()
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
	C.ghostty_terminal_resize(t.term, C.uint16_t(cols), C.uint16_t(rows), 8, 16)
}

// ScrollViewport scrolls the terminal viewport.
// action: 1=up, 2=down, 3=top, 4=bottom. amount is row count for up/down.
func (t *Terminal) ScrollViewport(action uint8, amount int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var behavior C.GhosttyTerminalScrollViewport
	switch action {
	case 1: // up
		behavior.tag = C.GHOSTTY_SCROLL_VIEWPORT_DELTA
		// CGO represents the C union as an opaque byte array.
		// Write intptr_t delta into the first bytes of the union.
		*(*C.intptr_t)(unsafe.Pointer(&behavior.value[0])) = C.intptr_t(-amount)
	case 2: // down
		behavior.tag = C.GHOSTTY_SCROLL_VIEWPORT_DELTA
		*(*C.intptr_t)(unsafe.Pointer(&behavior.value[0])) = C.intptr_t(amount)
	case 3: // top
		behavior.tag = C.GHOSTTY_SCROLL_VIEWPORT_TOP
	case 4: // bottom
		behavior.tag = C.GHOSTTY_SCROLL_VIEWPORT_BOTTOM
	default:
		return
	}
	C.ghostty_terminal_scroll_viewport(t.term, behavior)
}

// GetScrollbar returns the scrollbar state (total rows, viewport offset, viewport length).
func (t *Terminal) GetScrollbar() (total, offset, length uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var sb C.GhosttyTerminalScrollbar
	C.ghostty_terminal_get(t.term, C.GHOSTTY_TERMINAL_DATA_SCROLLBAR, unsafe.Pointer(&sb))
	return uint64(sb.total), uint64(sb.offset), uint64(sb.len)
}

// GetCursor returns the current cursor state
func (t *Terminal) GetCursor() CursorState {
	t.renderMu.Lock()
	defer t.renderMu.Unlock()

	t.updateRenderState()

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

// GetScreen returns the current screen content as a 2D grid of cells.
// Deprecated: use FillScreen for zero-allocation screen reading.
func (t *Terminal) GetScreen() [][]Cell {
	t.renderMu.Lock()
	defer t.renderMu.Unlock()

	t.updateRenderState()

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

	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, unsafe.Pointer(&t.rowIter))

	row := 0
	for C.ghostty_render_state_row_iterator_next(t.rowIter) && row < t.rows {
		C.ghostty_render_state_row_get(t.rowIter, C.GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, unsafe.Pointer(&t.rowCells))
		col := 0
		for C.ghostty_render_state_row_cells_next(t.rowCells) && col < t.cols {
			t.readCellInto(&screen[row][col])
			col++
		}
		row++
	}

	return screen
}

// FillScreen reads the terminal screen into a flat caller-owned buffer and cursor state.
// buf must have capacity for at least cols*rows cells.
// Returns false if the screen is not dirty (nothing changed since last call).
// When false is returned, buf and cursor are not modified.
func (t *Terminal) FillScreen(buf []Cell, cursor *CursorState) bool {
	return t.fillScreen(buf, cursor, false)
}

// ForceReadScreen reads the terminal screen unconditionally, ignoring dirty state.
// Use for initial subscribe and after resize where the client needs a full frame.
func (t *Terminal) ForceReadScreen(buf []Cell, cursor *CursorState) {
	t.fillScreen(buf, cursor, true)
}

// updateRenderState refreshes the render state from the terminal. This is the
// only step that needs access to the terminal instance, so it briefly takes
// t.mu; callers hold t.renderMu for the duration of the subsequent read so
// PTY writes (which also take t.mu) can proceed concurrently with the read.
func (t *Terminal) updateRenderState() {
	t.mu.Lock()
	C.ghostty_render_state_update(t.renderState, t.term)
	t.mu.Unlock()
}

// rowCellBuf returns a reusable C-side row buffer with at least n cells.
func (t *Terminal) rowCellBuf(n int) []C.DevmuxCell {
	if cap(t.rowBuf) < n {
		t.rowBuf = make([]C.DevmuxCell, n)
	}
	return t.rowBuf[:n]
}

func (t *Terminal) fillScreen(buf []Cell, cursor *CursorState, force bool) bool {
	t.renderMu.Lock()
	defer t.renderMu.Unlock()

	// Update render state from terminal (consumes terminal dirty flags).
	// Only this call touches the terminal; the reads below hit render state.
	t.updateRenderState()

	// Check global dirty state
	var dirty C.GhosttyRenderStateDirty
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_DIRTY, unsafe.Pointer(&dirty))

	if !force && dirty == C.GHOSTTY_RENDER_STATE_DIRTY_FALSE {
		return false
	}

	partial := !force && dirty == C.GHOSTTY_RENDER_STATE_DIRTY_PARTIAL

	// Read cursor state (cheap — a few CGO calls)
	var visible C.bool
	var hasValue C.bool
	var cx, cy C.uint16_t
	var style C.GhosttyRenderStateCursorVisualStyle

	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, unsafe.Pointer(&visible))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, unsafe.Pointer(&hasValue))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, unsafe.Pointer(&cx))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, unsafe.Pointer(&cy))
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_CURSOR_VISUAL_STYLE, unsafe.Pointer(&style))

	cursorStyle := CursorBlock
	switch style {
	case C.GHOSTTY_RENDER_STATE_CURSOR_VISUAL_STYLE_BAR:
		cursorStyle = CursorBar
	case C.GHOSTTY_RENDER_STATE_CURSOR_VISUAL_STYLE_UNDERLINE:
		cursorStyle = CursorUnderline
	}
	cursor.X = int(cx)
	cursor.Y = int(cy)
	cursor.Visible = bool(visible) && bool(hasValue)
	cursor.Style = cursorStyle

	// Iterate rows
	C.ghostty_render_state_get(t.renderState, C.GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, unsafe.Pointer(&t.rowIter))

	row := 0
	for C.ghostty_render_state_row_iterator_next(t.rowIter) && row < t.rows {
		rowOffset := row * t.cols

		// In partial mode, skip clean rows
		if partial {
			var rowDirty C.bool
			C.ghostty_render_state_row_get(t.rowIter, C.GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY, unsafe.Pointer(&rowDirty))
			if !bool(rowDirty) {
				row++
				continue
			}
			// Reset row dirty flag
			rowClean := C.bool(false)
			C.ghostty_render_state_row_set(t.rowIter, C.GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, unsafe.Pointer(&rowClean))
		}

		C.ghostty_render_state_row_get(t.rowIter, C.GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, unsafe.Pointer(&t.rowCells))

		// Read the whole row in one CGO call.
		rowBuf := t.rowCellBuf(t.cols)
		n := int(C.devmux_read_row(t.rowCells, &rowBuf[0], C.size_t(t.cols)))

		for col := 0; col < n; col++ {
			decodeCell(&rowBuf[col], &buf[rowOffset+col])
		}

		// Fill remaining cols as empty
		for col := n; col < t.cols; col++ {
			c := &buf[rowOffset+col]
			c.Char = ' '
			c.FG = Color{Default: true}
			c.BG = Color{Default: true}
			c.Bold = false
			c.Italic = false
			c.Underline = false
			c.Strikethrough = false
		}

		row++
	}

	// Reset global dirty state
	cleanState := C.GHOSTTY_RENDER_STATE_DIRTY_FALSE
	C.ghostty_render_state_set(t.renderState, C.GHOSTTY_RENDER_STATE_OPTION_DIRTY, unsafe.Pointer(&cleanState))

	return true
}

// decodeCell converts a C-side DevmuxCell (already read in bulk by
// devmux_read_row) into a Cell. This performs no CGO calls — it is plain
// memory access into the reusable row buffer.
func decodeCell(src *C.DevmuxCell, dst *Cell) {
	if src.codepoint == 0 {
		dst.Char = ' '
	} else {
		dst.Char = rune(src.codepoint)
	}

	if src.fg_default != 0 {
		dst.FG = Color{Default: true}
	} else {
		dst.FG = Color{R: uint8(src.fg_r), G: uint8(src.fg_g), B: uint8(src.fg_b)}
	}

	if src.bg_default != 0 {
		dst.BG = Color{Default: true}
	} else {
		dst.BG = Color{R: uint8(src.bg_r), G: uint8(src.bg_g), B: uint8(src.bg_b)}
	}

	f := uint8(src.flags)
	dst.Bold = f&uint8(C.DEVMUX_CELL_BOLD) != 0
	dst.Italic = f&uint8(C.DEVMUX_CELL_ITALIC) != 0
	dst.Underline = f&uint8(C.DEVMUX_CELL_UNDERLINE) != 0
	dst.Strikethrough = f&uint8(C.DEVMUX_CELL_STRIKETHROUGH) != 0
}

// readCellInto reads the current cell from the row cells iterator into dst.
// Must be called while rowCells is positioned on a valid cell.
func (t *Terminal) readCellInto(dst *Cell) {
	// Get grapheme
	var graphemeLen C.uint32_t
	C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, unsafe.Pointer(&graphemeLen))

	if graphemeLen > 0 {
		// Use fixed buffer for graphemes (covers 99.9% of cases)
		if graphemeLen <= C.uint32_t(len(t.graphemeBuf)) {
			C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, unsafe.Pointer(&t.graphemeBuf[0]))
			dst.Char = rune(t.graphemeBuf[0])
		} else {
			// Extremely rare: grapheme cluster > 8 codepoints, fall back to alloc
			buf := make([]C.uint32_t, graphemeLen)
			C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, unsafe.Pointer(&buf[0]))
			dst.Char = rune(buf[0])
		}
	} else {
		dst.Char = ' '
	}

	// Get foreground color
	var fgColor C.GhosttyColorRgb
	fgResult := C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, unsafe.Pointer(&fgColor))
	if fgResult == C.GHOSTTY_SUCCESS {
		dst.FG = Color{R: uint8(fgColor.r), G: uint8(fgColor.g), B: uint8(fgColor.b)}
	} else {
		dst.FG = Color{Default: true}
	}

	// Get background color
	var bgColor C.GhosttyColorRgb
	bgResult := C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, unsafe.Pointer(&bgColor))
	if bgResult == C.GHOSTTY_SUCCESS {
		dst.BG = Color{R: uint8(bgColor.r), G: uint8(bgColor.g), B: uint8(bgColor.b)}
	} else {
		dst.BG = Color{Default: true}
	}

	// Get style
	var cellStyle C.GhosttyStyle
	cellStyle.size = C.size_t(unsafe.Sizeof(cellStyle))
	C.ghostty_render_state_row_cells_get(t.rowCells, C.GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, unsafe.Pointer(&cellStyle))
	dst.Bold = bool(cellStyle.bold)
	dst.Italic = bool(cellStyle.italic)
	dst.Strikethrough = bool(cellStyle.strikethrough)
	dst.Underline = cellStyle.underline != 0
}

// Size returns the current terminal dimensions
func (t *Terminal) Size() (cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}
