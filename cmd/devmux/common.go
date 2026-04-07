package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"devmux/internal/protocol"
)

const defaultConfigName = "devmux.yaml"

func sendCommand(req protocol.Request) protocol.Response {
	conn, err := net.Dial(protocol.GetSocketNetwork(), protocol.GetSocketPath())
	if err != nil {
		if req.Command == "shutdown" {
			fmt.Println("Daemon is not running.")
			return protocol.Response{Status: "ok"}
		}
		fmt.Fprintf(os.Stderr, "Daemon is not running. Start it with: devmux start\n")
		os.Exit(1)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	if req.Command == "status" && resp.Status == "ok" && req.Name != "" {
		filtered := ""
		for _, line := range strings.Split(resp.Message, "\n") {
			if strings.HasPrefix(line, req.Name+":") {
				filtered = line
				break
			}
		}
		if filtered == "" {
			log.Fatalf("Process %s not found", req.Name)
		}
		resp.Message = filtered + "\n"
	}

	if req.Command != "dump" && req.Command != "logs" {
		fmt.Printf("%s: %s\n", resp.Status, resp.Message)
	}

	return resp
}
