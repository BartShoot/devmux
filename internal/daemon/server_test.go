package daemon

import (
	"encoding/json"
	"net"
	"testing"

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
