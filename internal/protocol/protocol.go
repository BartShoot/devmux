package protocol

// ============================================================================
// Legacy Protocol (kept for backward compatibility during migration)
// ============================================================================

type Request struct {
	Command string `json:"command"`
	Name    string `json:"name,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Input   string `json:"input,omitempty"` // for "input" command - data to send to process stdin
}

type Response struct {
	Status     string  `json:"status"`
	Message    string  `json:"message,omitempty"`
	TotalLines int     `json:"total_lines,omitempty"`
	Layout     *Layout `json:"layout,omitempty"`
}

// Layout describes the tab/pane structure for the TUI
type Layout struct {
	Tabs []TabLayout `json:"tabs"`
}

type TabLayout struct {
	Name   string       `json:"name"`
	Layout string       `json:"layout"` // "vertical", "horizontal", "split"
	Panes  []PaneLayout `json:"panes"`
}

type PaneLayout struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Status  string `json:"status"`
}

// ============================================================================
// New Streaming Protocol - Push-based with numerical IDs
// ============================================================================

// Numerical IDs for efficient internal use
type PaneID uint32
type TabID uint32

// Client -> Daemon message types
type ClientMsgType uint8

const (
	MsgGetLayout  ClientMsgType = 1
	MsgSubscribe  ClientMsgType = 2
	MsgUnsubscribe ClientMsgType = 3
	MsgInput      ClientMsgType = 4
	MsgMouse      ClientMsgType = 5
	MsgResize     ClientMsgType = 6
)

// Daemon -> Client message types
type ServerMsgType uint8

const (
	MsgLayout       ServerMsgType = 1
	MsgScreenUpdate ServerMsgType = 2
	MsgSelection    ServerMsgType = 3
	MsgPaneStatus   ServerMsgType = 4
	MsgError        ServerMsgType = 5
)

// ClientMessage is the envelope for all client-to-daemon messages
type ClientMessage struct {
	Type       ClientMsgType   `json:"type"`
	Subscribe  *SubscribeMsg   `json:"subscribe,omitempty"`
	Input      *InputMsg       `json:"input,omitempty"`
	Mouse      *MouseMsg       `json:"mouse,omitempty"`
	Resize     *ResizeMsg      `json:"resize,omitempty"`
}

// ServerMessage is the envelope for all daemon-to-client messages
type ServerMessage struct {
	Type         ServerMsgType   `json:"type"`
	Layout       *LayoutMsg      `json:"layout,omitempty"`
	ScreenUpdate *ScreenUpdate   `json:"screen_update,omitempty"`
	Selection    *SelectionMsg   `json:"selection,omitempty"`
	PaneStatus   *PaneStatusMsg  `json:"pane_status,omitempty"`
	Error        *ErrorMsg       `json:"error,omitempty"`
}

// SubscribeMsg requests screen updates for specific panes
type SubscribeMsg struct {
	PaneIDs []PaneID `json:"pane_ids"`
}

// InputMsg sends keyboard input to a pane
type InputMsg struct {
	PaneID PaneID `json:"pane_id"`
	Data   string `json:"data"`
}

// MouseAction represents mouse event types
type MouseAction uint8

const (
	MousePress   MouseAction = 1
	MouseDrag    MouseAction = 2
	MouseRelease MouseAction = 3
)

// MouseMsg sends mouse events to daemon for selection handling
type MouseMsg struct {
	PaneID PaneID      `json:"pane_id"`
	Action MouseAction `json:"action"`
	X      uint16      `json:"x"`
	Y      uint16      `json:"y"`
}

// ResizeMsg notifies daemon of terminal size change
type ResizeMsg struct {
	PaneID PaneID `json:"pane_id"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
}

// LayoutMsg contains tab/pane structure with numerical IDs
type LayoutMsg struct {
	Tabs []TabInfo `json:"tabs"`
}

// TabInfo describes a tab with numerical ID
type TabInfo struct {
	ID     TabID      `json:"id"`
	Name   string     `json:"name"`
	Layout string     `json:"layout"`
	Panes  []PaneInfo `json:"panes"`
}

// PaneInfo describes a pane with numerical ID
type PaneInfo struct {
	ID      PaneID `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Status  string `json:"status"`
}

// ScreenUpdate contains terminal screen state pushed from daemon
type ScreenUpdate struct {
	PaneID   PaneID     `json:"pane_id"`
	Sequence uint64     `json:"seq"`
	Full     bool       `json:"full"`
	Cols     uint16     `json:"cols"`
	Rows     uint16     `json:"rows"`
	Cells    []CellData `json:"cells"`
	Cursor   CursorData `json:"cursor"`
}

// CellData represents a single terminal cell
type CellData struct {
	Char  rune   `json:"c"`
	FG    Color  `json:"fg"`
	BG    Color  `json:"bg"`
	Attrs uint8  `json:"a"` // Bit flags: 1=bold, 2=italic, 4=underline, 8=strikethrough
}

// Color represents RGB color with default flag
type Color struct {
	R       uint8 `json:"r"`
	G       uint8 `json:"g"`
	B       uint8 `json:"b"`
	Default bool  `json:"d,omitempty"`
}

// Cell attribute flags
const (
	AttrBold          uint8 = 1 << 0
	AttrItalic        uint8 = 1 << 1
	AttrUnderline     uint8 = 1 << 2
	AttrStrikethrough uint8 = 1 << 3
)

// CursorData represents cursor position and visibility
type CursorData struct {
	X       uint16 `json:"x"`
	Y       uint16 `json:"y"`
	Visible bool   `json:"v"`
}

// SelectionMsg contains selection state changes
type SelectionMsg struct {
	PaneID   PaneID `json:"pane_id"`
	Active   bool   `json:"active"`
	StartX   uint16 `json:"sx,omitempty"`
	StartY   uint16 `json:"sy,omitempty"`
	EndX     uint16 `json:"ex,omitempty"`
	EndY     uint16 `json:"ey,omitempty"`
	Text     string `json:"text,omitempty"` // Selected text when selection completed
}

// PaneStatusMsg notifies of pane health/running status changes
type PaneStatusMsg struct {
	PaneID  PaneID `json:"pane_id"`
	Running bool   `json:"running"`
	Status  string `json:"status"`
}

// ErrorMsg reports errors to client
type ErrorMsg struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
