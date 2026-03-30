package tui

import (
	"encoding/json"
	"net"
	"sync"

	"devmux/internal/protocol"
)

// StreamClient manages the streaming connection to the daemon
type StreamClient struct {
	conn     net.Conn
	encoder  *json.Encoder
	decoder  *json.Decoder
	handlers StreamHandlers
	mu       sync.Mutex
	closed   bool
}

// StreamHandlers defines callbacks for server messages
type StreamHandlers struct {
	OnLayout       func(*protocol.LayoutMsg)
	OnScreenUpdate func(*protocol.ScreenUpdate)
	OnSelection    func(*protocol.SelectionMsg)
	OnPaneStatus   func(*protocol.PaneStatusMsg)
	OnError        func(*protocol.ErrorMsg)
}

// NewStreamClient creates a new streaming client
func NewStreamClient(network, addr string, handlers StreamHandlers) (*StreamClient, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	return &StreamClient{
		conn:     conn,
		encoder:  json.NewEncoder(conn),
		decoder:  json.NewDecoder(conn),
		handlers: handlers,
	}, nil
}

// Close closes the connection
func (c *StreamClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.conn.Close()
}

// ReceiveLoop reads messages from the server and dispatches to handlers
// This should be run in a goroutine
func (c *StreamClient) ReceiveLoop() error {
	for {
		var msg protocol.ServerMessage
		if err := c.decoder.Decode(&msg); err != nil {
			c.mu.Lock()
			closed := c.closed
			c.mu.Unlock()
			if closed {
				return nil
			}
			return err
		}

		switch msg.Type {
		case protocol.MsgLayout:
			if c.handlers.OnLayout != nil && msg.Layout != nil {
				c.handlers.OnLayout(msg.Layout)
			}
		case protocol.MsgScreenUpdate:
			if c.handlers.OnScreenUpdate != nil && msg.ScreenUpdate != nil {
				c.handlers.OnScreenUpdate(msg.ScreenUpdate)
			}
		case protocol.MsgSelection:
			if c.handlers.OnSelection != nil && msg.Selection != nil {
				c.handlers.OnSelection(msg.Selection)
			}
		case protocol.MsgPaneStatus:
			if c.handlers.OnPaneStatus != nil && msg.PaneStatus != nil {
				c.handlers.OnPaneStatus(msg.PaneStatus)
			}
		case protocol.MsgError:
			if c.handlers.OnError != nil && msg.Error != nil {
				c.handlers.OnError(msg.Error)
			}
		}
	}
}

// SendGetLayout requests the layout from the server
func (c *StreamClient) SendGetLayout() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{Type: protocol.MsgGetLayout})
}

// SendSubscribe subscribes to pane updates
func (c *StreamClient) SendSubscribe(paneIDs []protocol.PaneID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{
		Type:      protocol.MsgSubscribe,
		Subscribe: &protocol.SubscribeMsg{PaneIDs: paneIDs},
	})
}

// SendUnsubscribe unsubscribes from pane updates
func (c *StreamClient) SendUnsubscribe(paneIDs []protocol.PaneID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{
		Type:      protocol.MsgUnsubscribe,
		Subscribe: &protocol.SubscribeMsg{PaneIDs: paneIDs},
	})
}

// SendInput sends keyboard input to a pane
func (c *StreamClient) SendInput(paneID protocol.PaneID, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{
		Type:  protocol.MsgInput,
		Input: &protocol.InputMsg{PaneID: paneID, Data: data},
	})
}

// SendMouse sends a mouse event
func (c *StreamClient) SendMouse(paneID protocol.PaneID, action protocol.MouseAction, x, y int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{
		Type: protocol.MsgMouse,
		Mouse: &protocol.MouseMsg{
			PaneID: paneID,
			Action: action,
			X:      uint16(x),
			Y:      uint16(y),
		},
	})
}

// SendResize sends a resize event
func (c *StreamClient) SendResize(paneID protocol.PaneID, cols, rows int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(protocol.ClientMessage{
		Type: protocol.MsgResize,
		Resize: &protocol.ResizeMsg{
			PaneID: paneID,
			Cols:   uint16(cols),
			Rows:   uint16(rows),
		},
	})
}
