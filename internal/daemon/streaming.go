package daemon

import (
	"net"
	"os"
	"sync"
	"time"

	"devmux/internal/protocol"
	"github.com/creack/pty"
)

// StreamingClient represents a connected TUI client with persistent connection
type StreamingClient struct {
	id         uint64
	conn       net.Conn
	writer     *protocol.BinaryWriter
	reader     *protocol.BinaryReader
	server     *Server
	subscribed map[protocol.PaneID]bool
	sendCh     chan *protocol.ServerMessage
	mu         sync.Mutex
	closed     bool
	closeCh    chan struct{}
}

// ClientManager manages all streaming clients
type ClientManager struct {
	clients     map[uint64]*StreamingClient
	subscribers map[protocol.PaneID]map[uint64]*StreamingClient // paneID -> clientID -> client
	nextID      uint64
	mu          sync.RWMutex
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		clients:     make(map[uint64]*StreamingClient),
		subscribers: make(map[protocol.PaneID]map[uint64]*StreamingClient),
		nextID:      1,
	}
}

// AddClient creates and registers a new streaming client
func (cm *ClientManager) AddClient(conn net.Conn, server *Server) *StreamingClient {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	client := &StreamingClient{
		id:         cm.nextID,
		conn:       conn,
		writer:     protocol.NewBinaryWriter(conn),
		reader:     protocol.NewBinaryReader(conn),
		server:     server,
		subscribed: make(map[protocol.PaneID]bool),
		sendCh:     make(chan *protocol.ServerMessage, 1000),
		closeCh:    make(chan struct{}),
	}
	cm.nextID++
	cm.clients[client.id] = client

	return client
}

// RemoveClient removes a client and its subscriptions
func (cm *ClientManager) RemoveClient(client *StreamingClient) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Remove from all subscriptions
	for paneID := range client.subscribed {
		if subs, ok := cm.subscribers[paneID]; ok {
			delete(subs, client.id)
			if len(subs) == 0 {
				delete(cm.subscribers, paneID)
			}
		}
	}

	delete(cm.clients, client.id)
}

// Subscribe adds a client to pane subscriptions
func (cm *ClientManager) Subscribe(client *StreamingClient, paneIDs []protocol.PaneID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, paneID := range paneIDs {
		if cm.subscribers[paneID] == nil {
			cm.subscribers[paneID] = make(map[uint64]*StreamingClient)
		}
		cm.subscribers[paneID][client.id] = client
		client.subscribed[paneID] = true
	}
}

// Unsubscribe removes a client from pane subscriptions
func (cm *ClientManager) Unsubscribe(client *StreamingClient, paneIDs []protocol.PaneID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, paneID := range paneIDs {
		if subs, ok := cm.subscribers[paneID]; ok {
			delete(subs, client.id)
			if len(subs) == 0 {
				delete(cm.subscribers, paneID)
			}
		}
		delete(client.subscribed, paneID)
	}
}

// BroadcastToPane sends a message to all subscribers of a pane.
// Iterates under read lock to avoid allocating a slice.
func (cm *ClientManager) BroadcastToPane(paneID protocol.PaneID, msg *protocol.ServerMessage) {
	cm.mu.RLock()
	subs := cm.subscribers[paneID]
	for _, client := range subs {
		client.Send(msg)
	}
	cm.mu.RUnlock()
}

// Send queues a message to be sent to the client
func (c *StreamingClient) Send(msg *protocol.ServerMessage) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	select {
	case c.sendCh <- msg:
	default:
		// Channel full, drop message (client too slow)
		// This is expected during heavy output bursts
	}
}

// Run starts the client read and write loops
func (c *StreamingClient) Run() {
	go c.writeLoop()
	c.readLoop()
}

// Close closes the client connection
func (c *StreamingClient) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.closeCh)
	c.mu.Unlock()

	c.conn.Close()
	c.server.clientManager.RemoveClient(c)
}

// readLoop reads and processes incoming binary messages
func (c *StreamingClient) readLoop() {
	defer c.Close()

	for {
		msg, err := c.reader.ReadClientMessage()
		if err != nil {
			return // Connection closed or error
		}

		c.handleMessage(msg)
	}
}

// writeLoop sends queued messages to the client as binary frames
func (c *StreamingClient) writeLoop() {
	for {
		select {
		case msg := <-c.sendCh:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			err := c.writer.WriteServerMessage(msg)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case <-c.closeCh:
			return
		}
	}
}

// handleMessage processes a client message
func (c *StreamingClient) handleMessage(msg *protocol.ClientMessage) {
	switch msg.Type {
	case protocol.MsgGetLayout:
		c.handleGetLayout()
	case protocol.MsgSubscribe:
		if msg.Subscribe != nil {
			c.handleSubscribe(msg.Subscribe)
		}
	case protocol.MsgUnsubscribe:
		if msg.Subscribe != nil {
			c.handleUnsubscribe(msg.Subscribe)
		}
	case protocol.MsgInput:
		if msg.Input != nil {
			c.handleInput(msg.Input)
		}
	case protocol.MsgMouse:
		if msg.Mouse != nil {
			c.handleMouse(msg.Mouse)
		}
	case protocol.MsgResize:
		if msg.Resize != nil {
			c.handleResize(msg.Resize)
		}
	case protocol.MsgScroll:
		if msg.Scroll != nil {
			c.handleScroll(msg.Scroll)
		}
	case protocol.MsgProcessControl:
		if msg.ProcessControl != nil {
			c.handleProcessControl(msg.ProcessControl)
		}
	}
}

func (c *StreamingClient) handleGetLayout() {
	layout := c.server.getLayoutMsgWithStatus()
	c.Send(&protocol.ServerMessage{
		Type:   protocol.MsgLayout,
		Layout: layout,
	})
}

func (c *StreamingClient) handleSubscribe(msg *protocol.SubscribeMsg) {
	c.server.clientManager.Subscribe(c, msg.PaneIDs)

	// Send initial full screen update for each subscribed pane
	for _, paneID := range msg.PaneIDs {
		c.server.sendFullScreenUpdate(c, paneID)
	}
}

func (c *StreamingClient) handleUnsubscribe(msg *protocol.SubscribeMsg) {
	c.server.clientManager.Unsubscribe(c, msg.PaneIDs)
}

func (c *StreamingClient) handleInput(msg *protocol.InputMsg) {
	// Map PaneID to name and forward input
	name := c.server.getPaneName(msg.PaneID)
	if name != "" {
		c.server.pm.WriteInput(name, msg.Data)
	}
}

func (c *StreamingClient) handleMouse(msg *protocol.MouseMsg) {
	sel := c.server.selectionManager.GetSelection(msg.PaneID)

	switch msg.Action {
	case protocol.MousePress:
		sel.HandleMousePress(int(msg.X), int(msg.Y))
		c.broadcastSelectionUpdate(msg.PaneID, sel, false)

	case protocol.MouseDrag:
		sel.HandleMouseDrag(int(msg.X), int(msg.Y))
		c.broadcastSelectionUpdate(msg.PaneID, sel, false)

	case protocol.MouseRelease:
		sel.HandleMouseRelease(int(msg.X), int(msg.Y))
		// On release, include the selected text
		c.broadcastSelectionUpdate(msg.PaneID, sel, true)
	}
}

// broadcastSelectionUpdate sends selection state to all subscribers
func (c *StreamingClient) broadcastSelectionUpdate(paneID protocol.PaneID, sel *Selection, includeText bool) {
	var text string
	if includeText {
		// Get process terminal to extract text
		name := c.server.getPaneName(paneID)
		if name != "" {
			c.server.pm.mu.Lock()
			proc, exists := c.server.pm.processes[name]
			c.server.pm.mu.Unlock()

			if exists && proc.Terminal != nil {
				text = sel.GetSelectedText(proc.Terminal)
			}
		}
	}

	msg := &protocol.ServerMessage{
		Type:      protocol.MsgSelection,
		Selection: sel.ToProtocol(paneID, text),
	}

	c.server.clientManager.BroadcastToPane(paneID, msg)
}

func (c *StreamingClient) handleResize(msg *protocol.ResizeMsg) {
	// Store the size for future restarts
	c.server.setPaneSize(msg.PaneID, int(msg.Cols), int(msg.Rows))

	name := c.server.getPaneName(msg.PaneID)
	if name == "" {
		return
	}

	c.server.pm.mu.Lock()
	proc, exists := c.server.pm.processes[name]
	c.server.pm.mu.Unlock()

	if !exists || proc.Terminal == nil {
		return
	}

	// Resize the terminal
	proc.Terminal.Resize(int(msg.Cols), int(msg.Rows))

	// Also resize the PTY if available
	if proc.PTY != nil {
		setTerminalSize(proc.PTY, int(msg.Cols), int(msg.Rows))
	}

	// Send immediate screen update with new dimensions
	c.server.sendFullScreenUpdate(c, msg.PaneID)
}

func (c *StreamingClient) handleScroll(msg *protocol.ScrollMsg) {
	name := c.server.getPaneName(msg.PaneID)
	if name == "" {
		return
	}

	c.server.pm.mu.Lock()
	proc, exists := c.server.pm.processes[name]
	c.server.pm.mu.Unlock()

	if !exists || proc.Terminal == nil {
		return
	}

	proc.Terminal.ScrollViewport(uint8(msg.Action), int(msg.Amount))

	// Send the new viewport content
	c.server.sendFullScreenUpdate(c, msg.PaneID)
}

func (c *StreamingClient) handleProcessControl(msg *protocol.ProcessControlMsg) {
	name := c.server.getPaneName(msg.PaneID)
	if name == "" {
		return
	}

	var err error
	switch msg.Action {
	case protocol.ProcessStop:
		err = c.server.pm.StopProcess(name)
	case protocol.ProcessStart:
		err = c.server.pm.StartStopped(name)
	case protocol.ProcessRestart:
		err = c.server.pm.RestartProcess(name)
	}

	if err != nil {
		c.Send(&protocol.ServerMessage{
			Type:  protocol.MsgError,
			Error: &protocol.ErrorMsg{Message: err.Error()},
		})
		return
	}

	// Broadcast updated status
	c.server.pm.mu.Lock()
	proc, exists := c.server.pm.processes[name]
	running := exists && proc.Running
	status := ""
	if exists {
		status = string(proc.Status)
	}
	c.server.pm.mu.Unlock()

	c.server.BroadcastPaneStatus(msg.PaneID, running, status)
}

// setTerminalSize resizes a PTY to the given dimensions
func setTerminalSize(f *os.File, cols, rows int) {
	pty.Setsize(f, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

// UpdateCoalescer tracks dirty panes and materializes screen state only on flush (~60fps).
// This avoids building CellData slices on every stdout read — only on actual sends.
type UpdateCoalescer struct {
	dirty    map[protocol.PaneID]bool
	timer    *time.Timer
	server   *Server
	mu       sync.Mutex
	interval time.Duration
}

func NewUpdateCoalescer(server *Server) *UpdateCoalescer {
	return &UpdateCoalescer{
		dirty:    make(map[protocol.PaneID]bool),
		server:   server,
		interval: 16 * time.Millisecond, // ~60fps
	}
}

// MarkDirty marks a pane as needing a screen update on next flush.
// This is cheap — no allocation, no terminal state read.
func (uc *UpdateCoalescer) MarkDirty(paneID protocol.PaneID) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.dirty[paneID] = true

	// Start timer if not already running
	if uc.timer == nil {
		uc.timer = time.AfterFunc(uc.interval, uc.flush)
	}
}

// flush materializes screen state for dirty panes and sends to subscribers.
func (uc *UpdateCoalescer) flush() {
	uc.mu.Lock()
	dirty := uc.dirty
	uc.dirty = make(map[protocol.PaneID]bool)
	uc.timer = nil
	uc.mu.Unlock()

	for paneID := range dirty {
		update := uc.server.materializeScreenUpdate(paneID)
		if update != nil {
			uc.server.clientManager.BroadcastToPane(paneID, &protocol.ServerMessage{
				Type:         protocol.MsgScreenUpdate,
				ScreenUpdate: update,
			})
		}
	}
}
