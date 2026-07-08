//go:build cgo && ghostty

package terminal

import "testing"

// TestFillScreenDecodesCells verifies the batched C reader (devmux_read_row +
// decodeCell) returns correct characters, colors, and attributes.
func TestFillScreenDecodesCells(t *testing.T) {
	term, err := New(20, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer term.Close()

	// "Hi", then a bold 'B', then a red 'R', then reset and a plain 'x'.
	term.Write([]byte("Hi\x1b[1mB\x1b[0m\x1b[31mR\x1b[0mx"))

	buf := make([]Cell, 20*3)
	var cursor CursorState
	term.ForceReadScreen(buf, &cursor)

	// Row 0: H i B R x  ...
	if got := buf[0].Char; got != 'H' {
		t.Errorf("cell0 char = %q, want 'H'", got)
	}
	if got := buf[1].Char; got != 'i' {
		t.Errorf("cell1 char = %q, want 'i'", got)
	}

	if buf[2].Char != 'B' || !buf[2].Bold {
		t.Errorf("cell2 = %+v, want char 'B' bold", buf[2])
	}
	if buf[3].Bold {
		t.Errorf("cell3 (after reset) should not be bold: %+v", buf[3])
	}

	if buf[3].Char != 'R' || buf[3].FG.Default {
		t.Errorf("cell3 = %+v, want char 'R' with explicit fg", buf[3])
	}

	// Trailing cells should be blank spaces with default colors.
	if buf[5].Char != ' ' || !buf[5].FG.Default || !buf[5].BG.Default {
		t.Errorf("cell5 (blank) = %+v, want space with default colors", buf[5])
	}

	// Cursor should sit just after 'x' (col 5, row 0).
	if cursor.X != 5 || cursor.Y != 0 {
		t.Errorf("cursor = (%d,%d), want (5,0)", cursor.X, cursor.Y)
	}
}
