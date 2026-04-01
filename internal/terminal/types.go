package terminal

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
