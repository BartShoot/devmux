package protocol

import (
	"encoding/json"
	"testing"
)

func TestClientMessage_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  ClientMessage
	}{
		{
			name: "GetLayout",
			msg:  ClientMessage{Type: MsgGetLayout},
		},
		{
			name: "Subscribe",
			msg: ClientMessage{
				Type:      MsgSubscribe,
				Subscribe: &SubscribeMsg{PaneIDs: []PaneID{1, 2, 3}},
			},
		},
		{
			name: "Input",
			msg: ClientMessage{
				Type:  MsgInput,
				Input: &InputMsg{PaneID: 5, Data: "hello\n"},
			},
		},
		{
			name: "Mouse press",
			msg: ClientMessage{
				Type:  MsgMouse,
				Mouse: &MouseMsg{PaneID: 1, Action: MousePress, X: 10, Y: 20},
			},
		},
		{
			name: "Resize",
			msg: ClientMessage{
				Type:   MsgResize,
				Resize: &ResizeMsg{PaneID: 2, Cols: 120, Rows: 40},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var decoded ClientMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if decoded.Type != tt.msg.Type {
				t.Errorf("type mismatch: got %v, want %v", decoded.Type, tt.msg.Type)
			}
		})
	}
}

func TestServerMessage_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  ServerMessage
	}{
		{
			name: "Layout",
			msg: ServerMessage{
				Type: MsgLayout,
				Layout: &LayoutMsg{
					Tabs: []TabInfo{
						{
							ID:     1,
							Name:   "Main",
							Layout: "vertical",
							Panes: []PaneInfo{
								{ID: 1, Name: "app", Running: true, Status: "Healthy"},
								{ID: 2, Name: "logs", Running: true, Status: "Checking"},
							},
						},
					},
				},
			},
		},
		{
			name: "ScreenUpdate full",
			msg: ServerMessage{
				Type: MsgScreenUpdate,
				ScreenUpdate: &ScreenUpdate{
					PaneID:   1,
					Sequence: 42,
					Full:     true,
					Cols:     80,
					Rows:     24,
					Cells: []CellData{
						{Char: 'H', FG: Color{R: 255, G: 255, B: 255}, BG: Color{Default: true}, Attrs: AttrBold},
						{Char: 'i', FG: Color{Default: true}, BG: Color{Default: true}},
					},
					Cursor: CursorData{X: 2, Y: 0, Visible: true},
				},
			},
		},
		{
			name: "Selection",
			msg: ServerMessage{
				Type: MsgSelection,
				Selection: &SelectionMsg{
					PaneID: 1,
					Active: true,
					StartX: 5,
					StartY: 10,
					EndX:   20,
					EndY:   12,
					Text:   "selected text",
				},
			},
		},
		{
			name: "PaneStatus",
			msg: ServerMessage{
				Type: MsgPaneStatus,
				PaneStatus: &PaneStatusMsg{
					PaneID:  3,
					Running: false,
					Status:  "Stopped",
				},
			},
		},
		{
			name: "Error",
			msg: ServerMessage{
				Type:  MsgError,
				Error: &ErrorMsg{Code: 404, Message: "pane not found"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var decoded ServerMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if decoded.Type != tt.msg.Type {
				t.Errorf("type mismatch: got %v, want %v", decoded.Type, tt.msg.Type)
			}
		})
	}
}

func TestScreenUpdate_Size(t *testing.T) {
	// Test that a full 80x24 screen serializes to reasonable size
	cells := make([]CellData, 80*24)
	for i := range cells {
		cells[i] = CellData{
			Char:  'A',
			FG:    Color{Default: true},
			BG:    Color{Default: true},
			Attrs: 0,
		}
	}

	msg := ServerMessage{
		Type: MsgScreenUpdate,
		ScreenUpdate: &ScreenUpdate{
			PaneID:   1,
			Sequence: 1,
			Full:     true,
			Cols:     80,
			Rows:     24,
			Cells:    cells,
			Cursor:   CursorData{X: 0, Y: 0, Visible: true},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	t.Logf("Full 80x24 screen JSON size: %d bytes (%.1f KB)", len(data), float64(len(data))/1024)

	// Initial JSON implementation is ~160KB for full screen
	// Future optimizations: binary encoding (~15KB), diffs (<4KB)
	if len(data) > 200*1024 {
		t.Errorf("screen update too large: %d bytes", len(data))
	}
}
