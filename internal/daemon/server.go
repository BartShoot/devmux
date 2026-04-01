package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"devmux/internal/protocol"
	"devmux/internal/terminal"
)

type Server struct {
	socketPath       string
	pm               *ProcessManager
	layout           *protocol.Layout
	clientManager    *ClientManager
	coalescer        *UpdateCoalescer
	selectionManager *SelectionManager

	// Pane ID mappings
	paneNameToID map[string]protocol.PaneID
	paneIDToName map[protocol.PaneID]string
	nextPaneID   uint32
	paneMu       sync.RWMutex

	// Sequence numbers for screen updates
	paneSequence map[protocol.PaneID]*uint64

	// Last known terminal sizes per pane (for restarts)
	paneSizes map[protocol.PaneID]struct{ cols, rows int }

	// Reusable cell buffers per pane (avoids alloc on every flush)
	paneCellBufs map[protocol.PaneID]*protocol.ScreenUpdate
	// Reusable terminal cell buffers per pane (for FillScreen)
	paneTermBufs map[protocol.PaneID][]terminal.Cell
	sizeMu    sync.RWMutex
}

func NewServer(socketPath string, pm *ProcessManager, layout *protocol.Layout) *Server {
	s := &Server{
		socketPath:   socketPath,
		pm:           pm,
		layout:       layout,
		paneNameToID: make(map[string]protocol.PaneID),
		paneIDToName: make(map[protocol.PaneID]string),
		nextPaneID:   1,
		paneSequence: make(map[protocol.PaneID]*uint64),
	}
	s.clientManager = NewClientManager()
	s.coalescer = NewUpdateCoalescer(s)
	s.selectionManager = NewSelectionManager()
	s.paneSizes = make(map[protocol.PaneID]struct{ cols, rows int })
	s.paneCellBufs = make(map[protocol.PaneID]*protocol.ScreenUpdate)
	s.paneTermBufs = make(map[protocol.PaneID][]terminal.Cell)

	// Build pane ID mappings from layout
	if layout != nil {
		for _, tab := range layout.Tabs {
			for _, pane := range tab.Panes {
				id := protocol.PaneID(s.nextPaneID)
				s.nextPaneID++
				s.paneNameToID[pane.Name] = id
				s.paneIDToName[id] = pane.Name
				seq := uint64(0)
				s.paneSequence[id] = &seq
			}
		}
	}

	// Wire up process manager to server for notifications
	pm.SetServer(s)

	return s
}

func (s *Server) Start() error {
	network := protocol.GetSocketNetwork()
	address := protocol.GetSocketPath()

	// For Unix sockets, remove stale socket file if it exists
	if network == "unix" {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale socket: %w", err)
		}
	}

	l, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s socket %s: %w", network, address, err)
	}
	defer func() {
		l.Close()
		// Clean up Unix socket file on exit
		if network == "unix" {
			os.Remove(address)
		}
	}()

	fmt.Printf("Daemon listening on %s://%s\n", network, address)

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Printf("failed to accept connection: %v\n", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	// Peek at first byte to detect protocol:
	// Binary streaming starts with 'D' (from magic "DMX\x01")
	// Legacy JSON starts with '{' (0x7B)
	br := bufio.NewReader(conn)
	firstByte, err := br.Peek(1)
	if err != nil {
		conn.Close()
		return
	}

	if firstByte[0] == protocol.BinaryMagic[0] {
		// Binary streaming protocol — read and validate full magic
		var magic [4]byte
		if _, err := br.Read(magic[:]); err != nil || magic != protocol.BinaryMagic {
			conn.Close()
			return
		}
		s.handleBinaryStreamingConnection(conn, br)
		return
	}

	// Legacy JSON protocol
	decoder := json.NewDecoder(br)
	encoder := json.NewEncoder(conn)

	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		encoder.Encode(protocol.Response{Status: "error", Message: "invalid request"})
		conn.Close()
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		encoder.Encode(protocol.Response{Status: "error", Message: "invalid request"})
		conn.Close()
		return
	}

	defer conn.Close()
	s.handleLegacyRequest(conn, encoder, &req)
}

// handleBinaryStreamingConnection sets up a binary streaming client after magic is consumed
func (s *Server) handleBinaryStreamingConnection(conn net.Conn, br *bufio.Reader) {
	client := s.clientManager.AddClient(conn, s)
	// Override the reader to use the buffered reader (which already consumed the magic)
	client.reader = protocol.NewBinaryReader(br)
	client.Run()
}

// handleLegacyRequest handles a legacy protocol request
func (s *Server) handleLegacyRequest(conn net.Conn, encoder *json.Encoder, req *protocol.Request) {
	var resp protocol.Response
	switch req.Command {
	case "layout":
		layout := s.getLayoutWithStatus()
		resp = protocol.Response{Status: "ok", Layout: layout}
	case "status":
		s.pm.mu.Lock()
		message := ""
		for _, p := range s.pm.processes {
			message += fmt.Sprintf("%s: %s (Running: %v)\n", p.Name, p.Status, p.Running)
		}
		s.pm.mu.Unlock()
		resp = protocol.Response{Status: "ok", Message: message}
	case "restart":
		if req.Name == "" {
			resp = protocol.Response{Status: "error", Message: "process name is required for restart"}
		} else {
			if err := s.pm.RestartProcess(req.Name); err != nil {
				resp = protocol.Response{Status: "error", Message: fmt.Sprintf("failed to restart process: %v", err)}
			} else {
				resp = protocol.Response{Status: "ok", Message: fmt.Sprintf("process %s restarted", req.Name)}
			}
		}
	case "logs":
		if req.Name == "" {
			resp = protocol.Response{Status: "error", Message: "process name is required for logs"}
		} else {
			s.pm.mu.Lock()
			p, exists := s.pm.processes[req.Name]
			s.pm.mu.Unlock()
			if !exists {
				resp = protocol.Response{Status: "error", Message: fmt.Sprintf("process %s not found", req.Name)}
			} else {
				lines := p.Buffer.GetLines()
				totalLines := len(lines)

				// Apply tailing if requested
				if req.Tail > 0 {
					if req.Tail < totalLines {
						lines = lines[totalLines-req.Tail:]
					}
				} else if req.Offset > 0 {
					// Apply offset if requested and tailing is not used
					if req.Offset < totalLines {
						lines = lines[req.Offset:]
					} else {
						lines = []string{}
					}
				}

				message := ""
				for _, line := range lines {
					message += line + "\n"
				}
				resp = protocol.Response{Status: "ok", Message: message, TotalLines: totalLines}
			}
		}
	case "logs-raw":
		if req.Name == "" {
			resp = protocol.Response{Status: "error", Message: "process name is required for logs-raw"}
		} else {
			s.pm.mu.Lock()
			p, exists := s.pm.processes[req.Name]
			s.pm.mu.Unlock()
			if !exists {
				resp = protocol.Response{Status: "error", Message: fmt.Sprintf("process %s not found", req.Name)}
			} else {
				data, totalBytes, _ := p.Buffer.GetRaw(req.Offset)
				resp = protocol.Response{Status: "ok", Message: string(data), TotalLines: totalBytes}
			}
		}
	case "input":
		if req.Name == "" {
			resp = protocol.Response{Status: "error", Message: "process name is required for input"}
		} else if req.Input == "" {
			resp = protocol.Response{Status: "error", Message: "input data is required"}
		} else {
			if err := s.pm.WriteInput(req.Name, req.Input); err != nil {
				resp = protocol.Response{Status: "error", Message: fmt.Sprintf("failed to write input: %v", err)}
			} else {
				resp = protocol.Response{Status: "ok", Message: "input sent"}
			}
		}
	case "shutdown":
		resp = protocol.Response{Status: "ok", Message: "Daemon shutting down"}
		encoder.Encode(resp)
		fmt.Println("Shutdown command received, cleaning up...")
		s.pm.StopAll()
		os.Exit(0)
	default:
		resp = protocol.Response{Status: "error", Message: "unknown command"}
	}

	encoder.Encode(resp)
}

// getPaneName converts a PaneID to its name
func (s *Server) getPaneName(id protocol.PaneID) string {
	s.paneMu.RLock()
	defer s.paneMu.RUnlock()
	return s.paneIDToName[id]
}

// getPaneID converts a name to its PaneID
func (s *Server) getPaneID(name string) protocol.PaneID {
	s.paneMu.RLock()
	defer s.paneMu.RUnlock()
	return s.paneNameToID[name]
}

// setPaneSize stores the last known size for a pane
func (s *Server) setPaneSize(paneID protocol.PaneID, cols, rows int) {
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	s.paneSizes[paneID] = struct{ cols, rows int }{cols, rows}
}

// getPaneSize returns the last known size for a pane (0,0 if not set)
func (s *Server) getPaneSize(paneID protocol.PaneID) (cols, rows int) {
	s.sizeMu.RLock()
	defer s.sizeMu.RUnlock()
	if size, ok := s.paneSizes[paneID]; ok {
		return size.cols, size.rows
	}
	return 0, 0
}

// getLayoutMsgWithStatus returns the layout as LayoutMsg with numerical IDs
func (s *Server) getLayoutMsgWithStatus() *protocol.LayoutMsg {
	if s.layout == nil {
		return nil
	}

	s.pm.mu.Lock()
	defer s.pm.mu.Unlock()

	layout := &protocol.LayoutMsg{
		Tabs: make([]protocol.TabInfo, len(s.layout.Tabs)),
	}

	var tabID protocol.TabID = 1
	for i, tab := range s.layout.Tabs {
		layout.Tabs[i] = protocol.TabInfo{
			ID:     tabID,
			Name:   tab.Name,
			Layout: tab.Layout,
			Panes:  make([]protocol.PaneInfo, len(tab.Panes)),
		}
		tabID++

		for j, pane := range tab.Panes {
			paneID := s.paneNameToID[pane.Name]
			layout.Tabs[i].Panes[j] = protocol.PaneInfo{
				ID:   paneID,
				Name: pane.Name,
			}
			if p, exists := s.pm.processes[pane.Name]; exists {
				layout.Tabs[i].Panes[j].Command = p.Command
				layout.Tabs[i].Panes[j].Running = p.Running
				layout.Tabs[i].Panes[j].Status = string(p.Status)
			}
		}
	}

	return layout
}

// sendFullScreenUpdate sends a full screen update to a client, bypassing dirty tracking.
// Used for initial subscribe and after resize — the client always needs a complete frame.
func (s *Server) sendFullScreenUpdate(client *StreamingClient, paneID protocol.PaneID) {
	update := s.forceReadScreenUpdate(paneID)
	if update == nil {
		// No terminal yet, send empty placeholder
		seq := atomic.AddUint64(s.paneSequence[paneID], 1)
		update = &protocol.ScreenUpdate{
			PaneID:   paneID,
			Sequence: seq,
			Full:     true,
			Cols:     80,
			Rows:     24,
			Cells:    []protocol.CellData{},
			Cursor:   protocol.CursorData{X: 0, Y: 0, Visible: true},
		}
	}
	client.Send(&protocol.ServerMessage{
		Type:         protocol.MsgScreenUpdate,
		ScreenUpdate: update,
	})
}

// forceReadScreenUpdate reads terminal state unconditionally (ignoring dirty tracking).
func (s *Server) forceReadScreenUpdate(paneID protocol.PaneID) *protocol.ScreenUpdate {
	name := s.getPaneName(paneID)
	if name == "" {
		return nil
	}

	s.pm.mu.Lock()
	proc, exists := s.pm.processes[name]
	s.pm.mu.Unlock()

	if !exists || proc.Terminal == nil {
		return nil
	}

	cols, rows := proc.Terminal.Size()
	totalCells := cols * rows

	cellBuf := s.getTermCellBuf(paneID, totalCells)

	var cursor terminal.CursorState
	proc.Terminal.ForceReadScreen(cellBuf, &cursor)

	update := s.paneCellBufs[paneID]
	if update == nil || cap(update.Cells) < totalCells {
		update = &protocol.ScreenUpdate{
			Cells: make([]protocol.CellData, totalCells),
		}
		s.paneCellBufs[paneID] = update
	}
	update.Cells = update.Cells[:totalCells]

	for i, cell := range cellBuf {
		c := &update.Cells[i]
		c.Char = cell.Char
		c.FG.R = cell.FG.R
		c.FG.G = cell.FG.G
		c.FG.B = cell.FG.B
		c.FG.Default = cell.FG.Default
		c.BG.R = cell.BG.R
		c.BG.G = cell.BG.G
		c.BG.B = cell.BG.B
		c.BG.Default = cell.BG.Default

		var attrs uint8
		if cell.Bold {
			attrs |= protocol.AttrBold
		}
		if cell.Italic {
			attrs |= protocol.AttrItalic
		}
		if cell.Underline {
			attrs |= protocol.AttrUnderline
		}
		if cell.Strikethrough {
			attrs |= protocol.AttrStrikethrough
		}
		c.Attrs = attrs
	}

	seq := atomic.AddUint64(&proc.updateSeq, 1)

	update.PaneID = paneID
	update.Sequence = seq
	update.Full = true
	update.Cols = uint16(cols)
	update.Rows = uint16(rows)
	update.Cursor.X = uint16(cursor.X)
	update.Cursor.Y = uint16(cursor.Y)
	update.Cursor.Visible = cursor.Visible

	// Include scroll position
	total, offset, length := proc.Terminal.GetScrollbar()
	update.Scroll = &protocol.ScrollInfo{
		Total:  total,
		Offset: offset,
		Len:    length,
	}

	return update
}

// getTermCellBuf returns a reusable terminal cell buffer for the given pane, growing if needed.
func (s *Server) getTermCellBuf(paneID protocol.PaneID, size int) []terminal.Cell {
	buf := s.paneTermBufs[paneID]
	if cap(buf) < size {
		buf = make([]terminal.Cell, size)
		s.paneTermBufs[paneID] = buf
	}
	return buf[:size]
}

// materializeScreenUpdate reads terminal state for a pane into a reusable ScreenUpdate.
// Called only from coalescer flush — not on every stdout read.
// Returns nil if the pane doesn't exist or the terminal has no dirty state.
func (s *Server) materializeScreenUpdate(paneID protocol.PaneID) *protocol.ScreenUpdate {
	name := s.getPaneName(paneID)
	if name == "" {
		return nil
	}

	s.pm.mu.Lock()
	proc, exists := s.pm.processes[name]
	s.pm.mu.Unlock()

	if !exists || proc.Terminal == nil {
		return nil
	}

	cols, rows := proc.Terminal.Size()
	totalCells := cols * rows

	// Ensure we have a reusable terminal cell buffer for FillScreen
	cellBuf := s.getTermCellBuf(paneID, totalCells)

	// FillScreen returns false if nothing changed (dirty tracking)
	var cursor terminal.CursorState
	if !proc.Terminal.FillScreen(cellBuf, &cursor) {
		// Viewport content unchanged, but if scrolled up, send a scroll-info-only
		// update so the TUI can show updated line counts as new output arrives.
		total, offset, length := proc.Terminal.GetScrollbar()
		if total > 0 && offset+length < total {
			// Scrolled up — send lightweight update with just scroll info
			update := s.paneCellBufs[paneID]
			if update != nil && len(update.Cells) > 0 {
				seq := atomic.AddUint64(&proc.updateSeq, 1)
				update.Sequence = seq
				update.Scroll = &protocol.ScrollInfo{
					Total: total, Offset: offset, Len: length,
				}
				return update
			}
		}
		return nil
	}

	// Reuse or grow the per-pane protocol cell buffer
	update := s.paneCellBufs[paneID]
	if update == nil || cap(update.Cells) < totalCells {
		update = &protocol.ScreenUpdate{
			Cells: make([]protocol.CellData, totalCells),
		}
		s.paneCellBufs[paneID] = update
	}
	update.Cells = update.Cells[:totalCells]

	// Convert terminal cells to protocol cells in-place
	for i, cell := range cellBuf {
		c := &update.Cells[i]
		c.Char = cell.Char
		c.FG.R = cell.FG.R
		c.FG.G = cell.FG.G
		c.FG.B = cell.FG.B
		c.FG.Default = cell.FG.Default
		c.BG.R = cell.BG.R
		c.BG.G = cell.BG.G
		c.BG.B = cell.BG.B
		c.BG.Default = cell.BG.Default

		var attrs uint8
		if cell.Bold {
			attrs |= protocol.AttrBold
		}
		if cell.Italic {
			attrs |= protocol.AttrItalic
		}
		if cell.Underline {
			attrs |= protocol.AttrUnderline
		}
		if cell.Strikethrough {
			attrs |= protocol.AttrStrikethrough
		}
		c.Attrs = attrs
	}

	seq := atomic.AddUint64(&proc.updateSeq, 1)

	update.PaneID = paneID
	update.Sequence = seq
	update.Full = true
	update.Cols = uint16(cols)
	update.Rows = uint16(rows)
	update.Cursor.X = uint16(cursor.X)
	update.Cursor.Y = uint16(cursor.Y)
	update.Cursor.Visible = cursor.Visible

	// Include scroll position
	total, offset, length := proc.Terminal.GetScrollbar()
	update.Scroll = &protocol.ScrollInfo{
		Total:  total,
		Offset: offset,
		Len:    length,
	}

	return update
}

// BroadcastPaneStatus sends a pane status update to all subscribers
func (s *Server) BroadcastPaneStatus(paneID protocol.PaneID, running bool, status string) {
	s.clientManager.BroadcastToPane(paneID, &protocol.ServerMessage{
		Type: protocol.MsgPaneStatus,
		PaneStatus: &protocol.PaneStatusMsg{
			PaneID:  paneID,
			Running: running,
			Status:  status,
		},
	})
}

// getLayoutWithStatus returns the layout with current process status
func (s *Server) getLayoutWithStatus() *protocol.Layout {
	if s.layout == nil {
		return nil
	}

	// Create a copy with updated status
	layout := &protocol.Layout{
		Tabs: make([]protocol.TabLayout, len(s.layout.Tabs)),
	}

	s.pm.mu.Lock()
	defer s.pm.mu.Unlock()

	for i, tab := range s.layout.Tabs {
		layout.Tabs[i] = protocol.TabLayout{
			Name:   tab.Name,
			Layout: tab.Layout,
			Panes:  make([]protocol.PaneLayout, len(tab.Panes)),
		}
		for j, pane := range tab.Panes {
			layout.Tabs[i].Panes[j] = protocol.PaneLayout{
				Name: pane.Name,
			}
			if p, exists := s.pm.processes[pane.Name]; exists {
				layout.Tabs[i].Panes[j].Running = p.Running
				layout.Tabs[i].Panes[j].Status = string(p.Status)
			}
		}
	}

	return layout
}
