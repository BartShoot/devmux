package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

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
	socketPath string
	pm         *ProcessManager
	layout     *protocol.Layout
}

func NewServer(socketPath string, pm *ProcessManager, layout *protocol.Layout) *Server {
	return &Server{
		socketPath: socketPath,
		pm:         pm,
		layout:     layout,
	}
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

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	var req protocol.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(protocol.Response{Status: "error", Message: "invalid request"})
		return
	}

	var resp protocol.Response
	switch req.Command {
	case "layout":
		// Return layout with current process status
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
					// Buffer was likely cleared
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
		json.NewEncoder(conn).Encode(resp)
		fmt.Println("Shutdown command received, cleaning up...")
		s.pm.StopAll()
		os.Exit(0)
	default:
		resp = protocol.Response{Status: "error", Message: "unknown command"}
	}

	json.NewEncoder(conn).Encode(resp)
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
