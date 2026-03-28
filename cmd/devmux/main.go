package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"

	"devmux/internal/protocol"
	"devmux/internal/tui"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: devmux <command> [args]")
		os.Exit(1)
	}

	command := os.Args[1]

	if command == "start" {
		cmd := exec.Command("go", "run", "cmd/devmuxd/main.go")
		// On Windows, Start-Process or similar might be better for true detaching,
		// but for a simple "go run" backgrounding:
		if err := cmd.Start(); err != nil {
			log.Fatalf("Failed to start daemon: %v", err)
		}
		fmt.Printf("Daemon started with PID %d\n", cmd.Process.Pid)
		return
	}

	if command == "ui" {
		ui, err := tui.NewTUI("localhost:8888")
		if err != nil {
			log.Fatalf("Failed to start UI: %v", err)
		}
		if err := ui.Run(); err != nil {
			log.Fatalf("UI error: %v", err)
		}
		return
	}

	conn, err := net.Dial("tcp", "localhost:8888")
	if err != nil {
		if command == "stop" {
			fmt.Println("Daemon is not running.")
			return
		}
		log.Fatalf("Failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	req := protocol.Request{Command: command}
	if command == "stop" {
		req.Command = "shutdown"
	}
	
	if len(os.Args) > 2 {
		req.Name = os.Args[2]
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Printf("%s: %s\n", resp.Status, resp.Message)
}
