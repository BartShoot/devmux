package daemon

import (
	"encoding/json"
	"fmt"
	"net"

	"devmux/internal/protocol"
)

type Server struct {
	socketPath string
	pm         *ProcessManager
}

func NewServer(socketPath string, pm *ProcessManager) *Server {
	return &Server{
		socketPath: socketPath,
		pm:         pm,
	}
}

func (s *Server) Start() error {
	l, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		return fmt.Errorf("failed to listen on tcp socket: %w", err)
	}
	defer l.Close()

	fmt.Printf("Daemon listening on localhost:8888\n")

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
				if req.Offset < len(lines) {
					lines = lines[req.Offset:]
				} else {
					lines = []string{}
				}
				// Combine lines into a single message for simplicity in this MVP
				message := ""
				for _, line := range lines {
					message += line + "\n"
				}
				resp = protocol.Response{Status: "ok", Message: message}
			}
		}
	default:
		resp = protocol.Response{Status: "error", Message: "unknown command"}
	}

	json.NewEncoder(conn).Encode(resp)
}
