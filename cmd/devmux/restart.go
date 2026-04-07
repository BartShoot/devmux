package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a process",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		wait, _ := cmd.Flags().GetBool("wait")
		resp := sendCommand(protocol.Request{Command: "restart", Name: name})

		if wait && resp.Status == "ok" {
			fmt.Printf("Waiting for %s to become healthy...\n", name)
			if err := waitForHealthyStatus(name); err != nil {
				log.Fatalf("Failed waiting for healthy status: %v", err)
			}
			fmt.Printf("%s is now healthy\n", name)
		}
	},
}

func init() {
	restartCmd.Flags().BoolP("wait", "w", false, "Wait for the process to become healthy")
}

func waitForHealthyStatus(name string) error {
	timeout := 5 * time.Minute
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ps, err := getProcessStatus(name)
		if err != nil {
			return err
		}

		if ps.Health == "Healthy" {
			return nil
		}

		if !ps.Running {
			return fmt.Errorf("process %s exited before becoming healthy", name)
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("timeout waiting for %s to become healthy", name)
}

type processStatus struct {
	Health  string
	Running bool
}

func getProcessStatus(name string) (processStatus, error) {
	conn, err := net.Dial(protocol.GetSocketNetwork(), protocol.GetSocketPath())
	if err != nil {
		return processStatus{}, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	req := protocol.Request{Command: "status"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return processStatus{}, fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return processStatus{}, fmt.Errorf("failed to read response: %w", err)
	}

	for _, line := range strings.Split(resp.Message, "\n") {
		if strings.HasPrefix(line, name+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				statusPart := strings.TrimSpace(parts[1])
				var ps processStatus
				if idx := strings.Index(statusPart, " ("); idx > 0 {
					ps.Health = statusPart[:idx]
					ps.Running = strings.Contains(statusPart[idx:], "Running: true")
				} else {
					ps.Health = statusPart
				}
				return ps, nil
			}
		}
	}

	return processStatus{}, fmt.Errorf("process %s not found in status", name)
}
