package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"devmux/internal/protocol"
)

// GetSocketPath returns the appropriate socket path for the current platform.
// On Unix systems, uses Unix domain socket. On Windows, falls back to TCP.
func GetSocketPath() string {
	if runtime.GOOS == "windows" {
		return "localhost:8888"
	}

	// Prefer XDG_RUNTIME_DIR if available (systemd-based systems)
	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		return filepath.Join(xdgRuntime, "devmux.sock")
	}

	// Fallback to /tmp
	return "/tmp/devmux.sock"
}

// GetSocketNetwork returns the network type for the socket.
func GetSocketNetwork() string {
	if runtime.GOOS == "windows" {
		return "tcp"
	}
	return "unix"
}

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
	network := GetSocketNetwork()
	address := GetSocketPath()

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

// rawMessage is used for protocol detection
type rawMessage struct {
	Type    json.RawMessage `json:"type,omitempty"`
	Command string          `json:"command,omitempty"`
}

func (s *Server) handleConnection(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// Read first message to detect protocol
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		json.NewEncoder(conn).Encode(protocol.Response{Status: "error", Message: "invalid request"})
		conn.Close()
		return
	}

	// Detect protocol by checking which fields exist
	var msg rawMessage
	json.Unmarshal(raw, &msg)

	if msg.Type != nil && len(msg.Type) > 0 {
		// New streaming protocol - decode as ClientMessage
		var clientMsg protocol.ClientMessage
		if err := json.Unmarshal(raw, &clientMsg); err != nil {
			encoder.Encode(protocol.Response{Status: "error", Message: "invalid streaming message"})
			conn.Close()
			return
		}
		s.handleStreamingConnectionWithFirst(conn, decoder, encoder, &clientMsg)
		return
	}

	// Legacy protocol - decode as Request
	var req protocol.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		encoder.Encode(protocol.Response{Status: "error", Message: "invalid request"})
		conn.Close()
		return
	}

	// Legacy protocol handling
	defer conn.Close()
	s.handleLegacyRequest(conn, encoder, &req)
}

// handleStreamingConnectionWithFirst handles a streaming connection with the first message already decoded
func (s *Server) handleStreamingConnectionWithFirst(conn net.Conn, decoder *json.Decoder, encoder *json.Encoder, firstMsg *protocol.ClientMessage) {
	client := s.clientManager.AddClient(conn, s)
	client.encoder = encoder
	client.decoder = decoder

	// Handle the first message
	client.handleMessage(firstMsg)

	// Continue with normal read loop
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

				if req.Offset < totalLines {
					lines = lines[req.Offset:]
				} else if req.Offset > totalLines {
					lines = lines[:]
				} else {
					lines = []string{}
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
				layout.Tabs[i].Panes[j].Running = p.Running
				layout.Tabs[i].Panes[j].Status = string(p.Status)
			}
		}
	}

	return layout
}

// sendFullScreenUpdate sends a full screen update to a client
func (s *Server) sendFullScreenUpdate(client *StreamingClient, paneID protocol.PaneID) {
	name := s.getPaneName(paneID)
	if name == "" {
		return
	}

	// Get process and its terminal
	s.pm.mu.Lock()
	proc, exists := s.pm.processes[name]
	s.pm.mu.Unlock()

	if !exists || proc.Terminal == nil {
		// No terminal yet, send empty placeholder
		seq := atomic.AddUint64(s.paneSequence[paneID], 1)
		update := &protocol.ScreenUpdate{
			PaneID:   paneID,
			Sequence: seq,
			Full:     true,
			Cols:     80,
			Rows:     24,
			Cells:    []protocol.CellData{},
			Cursor:   protocol.CursorData{X: 0, Y: 0, Visible: true},
		}
		client.Send(&protocol.ServerMessage{
			Type:         protocol.MsgScreenUpdate,
			ScreenUpdate: update,
		})
		return
	}

	// Get terminal state
	screen := proc.Terminal.GetScreen()
	cursor := proc.Terminal.GetCursor()
	cols, rows := proc.Terminal.Size()

	// Convert to protocol format
	cells := make([]protocol.CellData, 0, cols*rows)
	for _, row := range screen {
		for _, cell := range row {
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

			cells = append(cells, protocol.CellData{
				Char: cell.Char,
				FG: protocol.Color{
					R:       cell.FG.R,
					G:       cell.FG.G,
					B:       cell.FG.B,
					Default: cell.FG.Default,
				},
				BG: protocol.Color{
					R:       cell.BG.R,
					G:       cell.BG.G,
					B:       cell.BG.B,
					Default: cell.BG.Default,
				},
				Attrs: attrs,
			})
		}
	}

	seq := atomic.AddUint64(s.paneSequence[paneID], 1)
	update := &protocol.ScreenUpdate{
		PaneID:   paneID,
		Sequence: seq,
		Full:     true,
		Cols:     uint16(cols),
		Rows:     uint16(rows),
		Cells:    cells,
		Cursor: protocol.CursorData{
			X:       uint16(cursor.X),
			Y:       uint16(cursor.Y),
			Visible: cursor.Visible,
		},
	}

	client.Send(&protocol.ServerMessage{
		Type:         protocol.MsgScreenUpdate,
		ScreenUpdate: update,
	})
}

// NotifyScreenUpdate queues a screen update for subscribers (called from process pump)
func (s *Server) NotifyScreenUpdate(paneID protocol.PaneID, update *protocol.ScreenUpdate) {
	s.coalescer.QueueUpdate(update)
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
