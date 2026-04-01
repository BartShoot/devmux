package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"devmux/internal/daemon"
	"devmux/internal/protocol"
	"devmux/internal/tui"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: devmux <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  start                  Start the daemon")
		fmt.Println("  stop                   Stop the daemon")
		fmt.Println("  ui                     Open the TUI")
		fmt.Println("  status [name]          Show process status (all or specific)")
		fmt.Println("  restart <name> [--wait]  Restart a process (--wait waits for healthy)")
		fmt.Println("  logs <name> [flags]    Show process logs")
		fmt.Println("    --tail N             Show only last N lines")
		fmt.Println("    --grep PATTERN       Filter lines containing PATTERN")
		fmt.Println("    -A N                 Show N lines after each match")
		fmt.Println("    -B N                 Show N lines before each match")
		fmt.Println("    -C N                 Show N lines before and after each match")
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
		// Use streaming TUI (works without CGO)
		ui := tui.NewStreamingTUI(daemon.GetSocketNetwork(), daemon.GetSocketPath())
		if err := ui.Run(); err != nil {
			log.Fatalf("UI error: %v", err)
		}
		return
	}

	conn, err := net.Dial(daemon.GetSocketNetwork(), daemon.GetSocketPath())
	if err != nil {
		if command == "stop" {
			fmt.Println("Daemon is not running.")
			return
		}
		log.Fatalf("Failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	// Parse arguments for restart command
	var processName string
	var waitForHealthy bool
	var tailN int
	var grepPattern string
	var ctxAfter, ctxBefore int

	switch command {
	case "restart":
		for _, arg := range os.Args[2:] {
			if arg == "--wait" || arg == "-w" {
				waitForHealthy = true
			} else if !strings.HasPrefix(arg, "-") {
				processName = arg
			}
		}
		if processName == "" {
			log.Fatal("Usage: devmux restart <name> [--wait]")
		}
	case "logs":
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--tail":
				i++
				if i >= len(args) {
					log.Fatal("--tail requires a number")
				}
				n, err := strconv.Atoi(args[i])
				if err != nil {
					log.Fatalf("--tail: invalid number %q", args[i])
				}
				tailN = n
			case "--grep":
				i++
				if i >= len(args) {
					log.Fatal("--grep requires a pattern")
				}
				grepPattern = args[i]
			case "-A":
				i++
				if i >= len(args) {
					log.Fatal("-A requires a number")
				}
				n, err := strconv.Atoi(args[i])
				if err != nil {
					log.Fatalf("-A: invalid number %q", args[i])
				}
				ctxAfter = n
			case "-B":
				i++
				if i >= len(args) {
					log.Fatal("-B requires a number")
				}
				n, err := strconv.Atoi(args[i])
				if err != nil {
					log.Fatalf("-B: invalid number %q", args[i])
				}
				ctxBefore = n
			case "-C":
				i++
				if i >= len(args) {
					log.Fatal("-C requires a number")
				}
				n, err := strconv.Atoi(args[i])
				if err != nil {
					log.Fatalf("-C: invalid number %q", args[i])
				}
				ctxBefore = n
				ctxAfter = n
			default:
				if !strings.HasPrefix(args[i], "-") {
					processName = args[i]
				} else {
					log.Fatalf("Unknown flag: %s", args[i])
				}
			}
		}
		if processName == "" {
			log.Fatal("Usage: devmux logs <name> [--tail N] [--grep PATTERN] [-A N] [-B N] [-C N]")
		}
	default:
		if len(os.Args) > 2 {
			processName = os.Args[2]
		}
	}

	req := protocol.Request{Command: command, Name: processName, Tail: tailN}
	if command == "stop" {
		req.Command = "shutdown"
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	// Apply client-side grep filtering for logs
	if command == "logs" && resp.Status == "ok" && grepPattern != "" {
		resp.Message = grepLines(resp.Message, grepPattern, ctxBefore, ctxAfter)
	}

	// Filter status output to a single service if name was provided
	if command == "status" && resp.Status == "ok" && processName != "" {
		filtered := ""
		for _, line := range strings.Split(resp.Message, "\n") {
			if strings.HasPrefix(line, processName+":") {
				filtered = line
				break
			}
		}
		if filtered == "" {
			log.Fatalf("Process %s not found", processName)
		}
		resp.Message = filtered
	}

	fmt.Printf("%s: %s\n", resp.Status, resp.Message)

	// Wait for healthy status if requested
	if command == "restart" && waitForHealthy && resp.Status == "ok" {
		fmt.Printf("Waiting for %s to become healthy...\n", processName)
		if err := waitForHealthyStatus(processName); err != nil {
			log.Fatalf("Failed waiting for healthy status: %v", err)
		}
		fmt.Printf("%s is now healthy\n", processName)
	}
}

// waitForHealthyStatus polls the daemon until the process becomes healthy
func waitForHealthyStatus(name string) error {
	timeout := 5 * time.Minute
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		status, err := getProcessStatus(name)
		if err != nil {
			return err
		}

		if status == "Healthy" {
			return nil
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("timeout waiting for %s to become healthy", name)
}

// getProcessStatus queries the daemon for a process's health status
func getProcessStatus(name string) (string, error) {
	conn, err := net.Dial(daemon.GetSocketNetwork(), daemon.GetSocketPath())
	if err != nil {
		return "", fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	req := protocol.Request{Command: "status"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse status message to find the process
	// Format: "name: Status (Running: true/false)\n"
	for _, line := range strings.Split(resp.Message, "\n") {
		if strings.HasPrefix(line, name+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				statusPart := strings.TrimSpace(parts[1])
				// Extract status before " (Running:"
				if idx := strings.Index(statusPart, " ("); idx > 0 {
					return statusPart[:idx], nil
				}
				return statusPart, nil
			}
		}
	}

	return "", fmt.Errorf("process %s not found in status", name)
}

// grepLines filters lines by pattern with optional before/after context.
func grepLines(text, pattern string, before, after int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	// Find matching line indices
	var matches []int
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			matches = append(matches, i)
		}
	}

	if len(matches) == 0 {
		return ""
	}

	// Build set of lines to include (matches + context)
	include := make([]bool, len(lines))
	for _, m := range matches {
		start := m - before
		if start < 0 {
			start = 0
		}
		end := m + after
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for i := start; i <= end; i++ {
			include[i] = true
		}
	}

	// Collect included lines, inserting "--" separator between non-contiguous groups
	var result []string
	prevIncluded := false
	for i, line := range lines {
		if include[i] {
			if !prevIncluded && len(result) > 0 {
				result = append(result, "--")
			}
			result = append(result, line)
			prevIncluded = true
		} else {
			prevIncluded = false
		}
	}

	return strings.Join(result, "\n") + "\n"
}
