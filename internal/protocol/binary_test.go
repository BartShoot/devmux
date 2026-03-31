package protocol

import (
	"bytes"
	"testing"
)

func TestScreenUpdateRoundTrip(t *testing.T) {
	original := &ServerMessage{
		Type: MsgScreenUpdate,
		ScreenUpdate: &ScreenUpdate{
			PaneID:   PaneID(1),
			Sequence: 42,
			Full:     true,
			Cols:     80,
			Rows:     24,
			Cells:    make([]CellData, 80*24),
			Cursor:   CursorData{X: 5, Y: 10, Visible: true},
		},
	}

	// Set a few non-empty cells (rest are zero-value = empty)
	original.ScreenUpdate.Cells[0] = CellData{
		Char:  'H',
		FG:    Color{R: 255, G: 0, B: 0},
		BG:    Color{Default: true},
		Attrs: AttrBold,
	}
	original.ScreenUpdate.Cells[1] = CellData{
		Char:  'i',
		FG:    Color{Default: true},
		BG:    Color{R: 0, G: 0, B: 128},
		Attrs: AttrItalic,
	}
	// Cell at position 160 (row 2, col 0)
	original.ScreenUpdate.Cells[160] = CellData{
		Char:  'X',
		FG:    Color{R: 128, G: 128, B: 128},
		BG:    Color{R: 64, G: 64, B: 64},
		Attrs: AttrUnderline | AttrStrikethrough,
	}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	if err := w.WriteServerMessage(original); err != nil {
		t.Fatalf("encode: %v", err)
	}

	encoded := buf.Len()
	jsonApprox := 90 * 80 * 24 // ~90 bytes/cell in JSON
	t.Logf("Binary encoded size: %d bytes (vs ~%d JSON estimate)", encoded, jsonApprox)

	r := NewBinaryReader(&buf)
	decoded, err := r.ReadServerMessage()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	su := decoded.ScreenUpdate
	if su.PaneID != 1 || su.Sequence != 42 || !su.Full {
		t.Errorf("header mismatch: pane=%d seq=%d full=%v", su.PaneID, su.Sequence, su.Full)
	}
	if su.Cols != 80 || su.Rows != 24 {
		t.Errorf("dimensions mismatch: %dx%d", su.Cols, su.Rows)
	}
	if su.Cursor.X != 5 || su.Cursor.Y != 10 || !su.Cursor.Visible {
		t.Errorf("cursor mismatch: (%d,%d) visible=%v", su.Cursor.X, su.Cursor.Y, su.Cursor.Visible)
	}
	if len(su.Cells) != 80*24 {
		t.Fatalf("cell count: %d", len(su.Cells))
	}

	// Check non-empty cells
	c0 := su.Cells[0]
	if c0.Char != 'H' || c0.FG.R != 255 || c0.Attrs != AttrBold {
		t.Errorf("cell 0: %+v", c0)
	}
	c1 := su.Cells[1]
	if c1.Char != 'i' || c1.BG.B != 128 || c1.Attrs != AttrItalic {
		t.Errorf("cell 1: %+v", c1)
	}
	c160 := su.Cells[160]
	if c160.Char != 'X' || c160.Attrs != (AttrUnderline|AttrStrikethrough) {
		t.Errorf("cell 160: %+v", c160)
	}

	// Check that empty cells are properly decoded
	c2 := su.Cells[2]
	if c2.Char != ' ' || !c2.FG.Default || !c2.BG.Default || c2.Attrs != 0 {
		t.Errorf("cell 2 (should be empty): %+v", c2)
	}
}

func TestScreenUpdateSparseEncoding(t *testing.T) {
	// A mostly-empty 80x24 screen with just 3 cells should be tiny
	su := &ScreenUpdate{
		PaneID: 1, Sequence: 1, Full: true,
		Cols: 80, Rows: 24,
		Cells:  make([]CellData, 80*24),
		Cursor: CursorData{Visible: true},
	}
	// All cells default (empty)
	for i := range su.Cells {
		su.Cells[i] = CellData{Char: ' ', FG: Color{Default: true}, BG: Color{Default: true}}
	}
	// Set 3 non-empty
	su.Cells[0] = CellData{Char: 'A', FG: Color{R: 255}, Attrs: AttrBold}
	su.Cells[1] = CellData{Char: 'B', FG: Color{R: 255}, Attrs: AttrBold}
	su.Cells[80] = CellData{Char: 'C', FG: Color{Default: true}, BG: Color{R: 128}}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	if err := w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	size := buf.Len()
	// 3 explicit cells (12 bytes each) + skip opcodes (2 bytes each, a few of them) + header
	// Should be well under 200 bytes
	t.Logf("Sparse screen encoded: %d bytes (3 cells out of %d)", size, 80*24)
	if size > 300 {
		t.Errorf("sparse encoding too large: %d bytes, expected < 300", size)
	}
}

func TestLayoutRoundTrip(t *testing.T) {
	original := &ServerMessage{
		Type: MsgLayout,
		Layout: &LayoutMsg{
			Tabs: []TabInfo{
				{
					ID: 1, Name: "backend", Layout: "split",
					Panes: []PaneInfo{
						{ID: 1, Name: "server", Running: true, Status: "healthy"},
						{ID: 2, Name: "worker", Running: false, Status: "unhealthy"},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	if err := w.WriteServerMessage(original); err != nil {
		t.Fatalf("encode: %v", err)
	}

	r := NewBinaryReader(&buf)
	decoded, err := r.ReadServerMessage()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	l := decoded.Layout
	if len(l.Tabs) != 1 || l.Tabs[0].Name != "backend" {
		t.Errorf("layout: %+v", l)
	}
	if len(l.Tabs[0].Panes) != 2 {
		t.Fatalf("pane count: %d", len(l.Tabs[0].Panes))
	}
	p0 := l.Tabs[0].Panes[0]
	if p0.Name != "server" || !p0.Running || p0.Status != "healthy" {
		t.Errorf("pane 0: %+v", p0)
	}
}

func TestClientMessageRoundTrip(t *testing.T) {
	messages := []ClientMessage{
		{Type: MsgGetLayout},
		{Type: MsgSubscribe, Subscribe: &SubscribeMsg{PaneIDs: []PaneID{1, 2, 3}}},
		{Type: MsgInput, Input: &InputMsg{PaneID: 1, Data: "hello\n"}},
		{Type: MsgMouse, Mouse: &MouseMsg{PaneID: 1, Action: MousePress, X: 10, Y: 5}},
		{Type: MsgResize, Resize: &ResizeMsg{PaneID: 1, Cols: 120, Rows: 40}},
	}

	for _, orig := range messages {
		var buf bytes.Buffer
		w := NewBinaryWriter(&buf)
		if err := w.WriteClientMessage(&orig); err != nil {
			t.Fatalf("encode type %d: %v", orig.Type, err)
		}

		r := NewBinaryReader(&buf)
		decoded, err := r.ReadClientMessage()
		if err != nil {
			t.Fatalf("decode type %d: %v", orig.Type, err)
		}

		if decoded.Type != orig.Type {
			t.Errorf("type mismatch: got %d want %d", decoded.Type, orig.Type)
		}

		switch orig.Type {
		case MsgSubscribe:
			if len(decoded.Subscribe.PaneIDs) != 3 {
				t.Errorf("subscribe pane count: %d", len(decoded.Subscribe.PaneIDs))
			}
		case MsgInput:
			if decoded.Input.Data != "hello\n" {
				t.Errorf("input data: %q", decoded.Input.Data)
			}
		case MsgResize:
			if decoded.Resize.Cols != 120 || decoded.Resize.Rows != 40 {
				t.Errorf("resize: %dx%d", decoded.Resize.Cols, decoded.Resize.Rows)
			}
		}
	}
}

func BenchmarkScreenUpdateEncode(b *testing.B) {
	su := &ServerMessage{
		Type: MsgScreenUpdate,
		ScreenUpdate: &ScreenUpdate{
			PaneID: 1, Sequence: 1, Full: true,
			Cols: 80, Rows: 24,
			Cells:  make([]CellData, 80*24),
			Cursor: CursorData{X: 0, Y: 0, Visible: true},
		},
	}
	// Simulate typical terminal: first 40 cols of first 10 rows have content
	for row := 0; row < 10; row++ {
		for col := 0; col < 40; col++ {
			idx := row*80 + col
			su.ScreenUpdate.Cells[idx] = CellData{
				Char:  rune('a' + col%26),
				FG:    Color{R: 200, G: 200, B: 200},
				BG:    Color{Default: true},
				Attrs: 0,
			}
		}
	}
	// Rest are empty
	for i := range su.ScreenUpdate.Cells {
		c := &su.ScreenUpdate.Cells[i]
		if c.Char == 0 {
			c.Char = ' '
			c.FG.Default = true
			c.BG.Default = true
		}
	}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		w.WriteServerMessage(su)
	}
	b.SetBytes(int64(buf.Len()))
}
