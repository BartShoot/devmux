package daemon

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"devmux/internal/protocol"
)

func TestServer_HandleConnection_Status(t *testing.T) {
	pm := NewProcessManager()
	server := NewServer("unused", pm, nil)

	// Simulate a connection
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Run the handler in a goroutine
	go server.handleConnection(serverConn)

	// Send a status request
	req := protocol.Request{Command: "status"}
	err := json.NewEncoder(clientConn).Encode(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	// Read the response
	var resp protocol.Response
	err = json.NewDecoder(clientConn).Decode(&resp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}
}

func TestServer_HandleConnection_InvalidRequest(t *testing.T) {
	pm := NewProcessManager()
	server := NewServer("unused", pm, nil)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go server.handleConnection(serverConn)

	// Send invalid JSON
	clientConn.Write([]byte("{invalid-json}"))

	var resp protocol.Response
	err := json.NewDecoder(clientConn).Decode(&resp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", resp.Status)
	}
}

func TestServer_StreamingProtocol_GetLayout(t *testing.T) {
	pm := NewProcessManager()
	layout := &protocol.Layout{
		Tabs: []protocol.TabLayout{
			{
				Name:   "Main",
				Layout: "vertical",
				Panes: []protocol.PaneLayout{
					{Name: "app"},
					{Name: "logs"},
				},
			},
		},
	}
	server := NewServer("unused", pm, layout)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go server.handleConnection(serverConn)

	// Send GetLayout message (streaming protocol)
	msg := protocol.ClientMessage{Type: protocol.MsgGetLayout}
	err := json.NewEncoder(clientConn).Encode(msg)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	// Read the response (should be LayoutMsg)
	var resp protocol.ServerMessage
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = json.NewDecoder(clientConn).Decode(&resp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Type != protocol.MsgLayout {
		t.Errorf("Expected MsgLayout, got %v", resp.Type)
	}

	if resp.Layout == nil {
		t.Fatal("Expected layout in response")
	}

	if len(resp.Layout.Tabs) != 1 {
		t.Errorf("Expected 1 tab, got %d", len(resp.Layout.Tabs))
	}

	if resp.Layout.Tabs[0].Name != "Main" {
		t.Errorf("Expected tab name 'Main', got '%s'", resp.Layout.Tabs[0].Name)
	}

	if len(resp.Layout.Tabs[0].Panes) != 2 {
		t.Errorf("Expected 2 panes, got %d", len(resp.Layout.Tabs[0].Panes))
	}

	// Verify panes have numerical IDs
	if resp.Layout.Tabs[0].Panes[0].ID == 0 {
		t.Error("Expected pane to have non-zero ID")
	}
}

func TestServer_StreamingProtocol_Subscribe(t *testing.T) {
	pm := NewProcessManager()
	layout := &protocol.Layout{
		Tabs: []protocol.TabLayout{
			{
				Name:   "Main",
				Layout: "vertical",
				Panes: []protocol.PaneLayout{
					{Name: "app"},
				},
			},
		},
	}
	server := NewServer("unused", pm, layout)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go server.handleConnection(serverConn)

	// First get layout to know the pane ID
	msg := protocol.ClientMessage{Type: protocol.MsgGetLayout}
	json.NewEncoder(clientConn).Encode(msg)

	var layoutResp protocol.ServerMessage
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	json.NewDecoder(clientConn).Decode(&layoutResp)

	paneID := layoutResp.Layout.Tabs[0].Panes[0].ID

	// Subscribe to the pane
	subMsg := protocol.ClientMessage{
		Type: protocol.MsgSubscribe,
		Subscribe: &protocol.SubscribeMsg{
			PaneIDs: []protocol.PaneID{paneID},
		},
	}
	err := json.NewEncoder(clientConn).Encode(subMsg)
	if err != nil {
		t.Fatalf("Failed to send subscribe: %v", err)
	}

	// Should receive initial screen update
	var screenResp protocol.ServerMessage
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = json.NewDecoder(clientConn).Decode(&screenResp)
	if err != nil {
		t.Fatalf("Failed to decode screen update: %v", err)
	}

	if screenResp.Type != protocol.MsgScreenUpdate {
		t.Errorf("Expected MsgScreenUpdate, got %v", screenResp.Type)
	}

	if screenResp.ScreenUpdate == nil {
		t.Fatal("Expected screen update in response")
	}

	if screenResp.ScreenUpdate.PaneID != paneID {
		t.Errorf("Expected pane ID %d, got %d", paneID, screenResp.ScreenUpdate.PaneID)
	}

	if !screenResp.ScreenUpdate.Full {
		t.Error("Expected full screen update")
	}
}
