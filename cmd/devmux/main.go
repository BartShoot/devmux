package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"

	"devmux/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: devmux <command> [args]")
		os.Exit(1)
	}

	command := os.Args[1]

	conn, err := net.Dial("tcp", "localhost:8888")
	if err != nil {
		log.Fatalf("Failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	req := protocol.Request{Command: command}
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
