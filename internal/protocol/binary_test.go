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

func TestServerMessageExtraRoundTrip(t *testing.T) {
	messages := []ServerMessage{
		{
			Type: MsgSelection,
			Selection: &SelectionMsg{
				PaneID: 42,
				Active: true,
				StartX: 10, StartY: 5,
				EndX: 20, EndY: 8,
				Text: "selected text",
			},
		},
		{
			Type: MsgPaneStatus,
			PaneStatus: &PaneStatusMsg{
				PaneID:  123,
				Running: true,
				Status:  "All systems go",
			},
		},
		{
			Type: MsgError,
			Error: &ErrorMsg{
				Code:    500,
				Message: "Internal Server Error",
			},
		},
	}

	for _, orig := range messages {
		var buf bytes.Buffer
		w := NewBinaryWriter(&buf)
		if err := w.WriteServerMessage(&orig); err != nil {
			t.Fatalf("encode type %d: %v", orig.Type, err)
		}

		r := NewBinaryReader(&buf)
		decoded, err := r.ReadServerMessage()
		if err != nil {
			t.Fatalf("decode type %d: %v", orig.Type, err)
		}

		if decoded.Type != orig.Type {
			t.Errorf("type mismatch: got %d want %d", decoded.Type, orig.Type)
		}

		switch orig.Type {
		case MsgSelection:
			if decoded.Selection.Text != orig.Selection.Text {
				t.Errorf("selection text mismatch: %q != %q", decoded.Selection.Text, orig.Selection.Text)
			}
		case MsgPaneStatus:
			if decoded.PaneStatus.Status != orig.PaneStatus.Status {
				t.Errorf("pane status mismatch: %q != %q", decoded.PaneStatus.Status, orig.PaneStatus.Status)
			}
		case MsgError:
			if decoded.Error.Message != orig.Error.Message {
				t.Errorf("error mismatch: %q != %q", decoded.Error.Message, orig.Error.Message)
			}
		}
	}
}

func TestClientMessageExtraRoundTrip(t *testing.T) {
	messages := []ClientMessage{
		{
			Type:      MsgUnsubscribe,
			Subscribe: &SubscribeMsg{PaneIDs: []PaneID{10, 20}},
		},
		{
			Type:   MsgScroll,
			Scroll: &ScrollMsg{PaneID: 5, Action: ScrollUp, Amount: 10},
		},
		{
			Type:           MsgProcessControl,
			ProcessControl: &ProcessControlMsg{PaneID: 7, Action: ProcessRestart},
		},
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
		case MsgUnsubscribe:
			if len(decoded.Subscribe.PaneIDs) != 2 {
				t.Errorf("unsubscribe pane count mismatch")
			}
		case MsgScroll:
			if decoded.Scroll.Amount != 10 {
				t.Errorf("scroll amount mismatch")
			}
		case MsgProcessControl:
			if decoded.ProcessControl.Action != ProcessRestart {
				t.Errorf("process control action mismatch")
			}
		}
	}
}

func TestScreenUpdateComplexRoundTrip(t *testing.T) {
	su := &ScreenUpdate{
		PaneID:   99,
		Sequence: 1000,
		Full:     false,
		Cols:     10,
		Rows:     5,
		Cells:    make([]CellData, 50),
		Cursor:   CursorData{X: 2, Y: 3, Visible: false},
		Scroll: &ScrollInfo{
			Total:  5000,
			Offset: 4950,
			Len:    50,
		},
	}

	// Fill with a pattern that exercises all fields
	for i := range su.Cells {
		if i%3 == 0 {
			// Skip these (they will be ' ' with default colors)
			su.Cells[i] = CellData{Char: ' ', FG: Color{Default: true}, BG: Color{Default: true}}
		} else {
			su.Cells[i] = CellData{
				Char:  rune('A' + i),
				FG:    Color{R: uint8(i), G: uint8(i * 2), B: uint8(i * 3)},
				BG:    Color{R: uint8(255 - i), G: uint8(255 - i*2), B: uint8(255 - i*3)},
				Attrs: uint8(i % 16),
			}
		}
	}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	if err := w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	r := NewBinaryReader(&buf)
	decoded, err := r.ReadServerMessage()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	suDec := decoded.ScreenUpdate
	if suDec.Scroll == nil || suDec.Scroll.Total != su.Scroll.Total {
		t.Errorf("scroll info mismatch")
	}

	if len(suDec.Cells) != len(su.Cells) {
		t.Fatalf("cell count mismatch")
	}

	for i := range su.Cells {
		if suDec.Cells[i].Char != su.Cells[i].Char || suDec.Cells[i].Attrs != su.Cells[i].Attrs {
			t.Errorf("cell %d mismatch: got %+v, want %+v", i, suDec.Cells[i], su.Cells[i])
		}
	}
}

func TestReadServerMessageReuse(t *testing.T) {
	su1 := &ScreenUpdate{
		PaneID: 1, Sequence: 1, Full: true, Cols: 80, Rows: 24,
		Cells: make([]CellData, 80*24),
	}
	su1.Cells[0] = CellData{Char: 'A'}

	su2 := &ScreenUpdate{
		PaneID: 1, Sequence: 2, Full: true, Cols: 80, Rows: 24,
		Cells: make([]CellData, 80*24),
	}
	su2.Cells[0] = CellData{Char: 'B'}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su1})
	w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su2})

	r := NewBinaryReader(&buf)
	reuse := make(map[PaneID]*ScreenUpdate)

	msg1, err := r.ReadServerMessageReuse(reuse)
	if err != nil {
		t.Fatal(err)
	}
	addr1 := &msg1.ScreenUpdate.Cells[0]

	msg2, err := r.ReadServerMessageReuse(reuse)
	if err != nil {
		t.Fatal(err)
	}
	addr2 := &msg2.ScreenUpdate.Cells[0]

	if addr1 != addr2 {
		t.Errorf("expected cell buffer reuse, but addresses differ: %p != %p", addr1, addr2)
	}

	if msg2.ScreenUpdate.Cells[0].Char != 'B' {
		t.Errorf("expected 'B' in reused buffer, got %q", msg2.ScreenUpdate.Cells[0].Char)
	}
}

func TestSparseEncodingBoundaries(t *testing.T) {
	// 1. Test exactly 255 skips
	su := &ScreenUpdate{
		PaneID: 1, Sequence: 1, Full: true, Cols: 256, Rows: 2,
		Cells: make([]CellData, 512),
	}
	// Index 0: Content
	su.Cells[0] = CellData{Char: 'X'}
	// Indices 1 to 255: Empty (255 cells)
	for i := 1; i <= 255; i++ {
		su.Cells[i] = CellData{Char: ' ', FG: Color{Default: true}, BG: Color{Default: true}}
	}
	// Index 256: Content
	su.Cells[256] = CellData{Char: 'Y'}

	var buf bytes.Buffer
	w := NewBinaryWriter(&buf)
	w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su})

	r := NewBinaryReader(&buf)
	decoded, err := r.ReadServerMessage()
	if err != nil {
		t.Fatal(err)
	}

	if decoded.ScreenUpdate.Cells[256].Char != 'Y' {
		t.Errorf("expected 'Y' at 256, got %q", decoded.ScreenUpdate.Cells[256].Char)
	}

	// 2. Test 256 skips (should result in two skip opcodes: 255 + 1)
	su.Cells[256] = CellData{Char: ' ', FG: Color{Default: true}, BG: Color{Default: true}}
	su.Cells[257] = CellData{Char: 'Z'}

	buf.Reset()
	w.WriteServerMessage(&ServerMessage{Type: MsgScreenUpdate, ScreenUpdate: su})

	decoded, err = r.ReadServerMessage()
	if err != nil {
		t.Fatal(err)
	}

	if decoded.ScreenUpdate.Cells[257].Char != 'Z' {
		t.Errorf("expected 'Z' at 257, got %q", decoded.ScreenUpdate.Cells[257].Char)
	}
}
