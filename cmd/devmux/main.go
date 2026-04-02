package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"devmux/internal/config"
	"devmux/internal/protocol"
	"devmux/internal/tui"
)

const defaultConfigName = "devmux.yaml"

const sampleConfig = `# devmux configuration
# See https://github.com/BartShoot/devmux for documentation.

# Global working directory (optional). Relative pane/tab cwds resolve against this.
# cwd: /home/user/projects

tabs:
  - name: "app"
    # layout: "split"  # "vertical" (default), "horizontal", or "split"
    panes:
      - name: "server"
        command: "go run ./cmd/server"
        # cwd: "backend"  # relative to tab or global cwd
        health_check:
          type: "http"
          url: "http://localhost:8080/health"
          interval: 5s
          timeout: 30s

      - name: "worker"
        command: "go run ./cmd/worker"
        health_check:
          type: "tcp"
          address: "localhost:9090"
          interval: 5s
          timeout: 30s

  - name: "frontend"
    panes:
      - name: "dev-server"
        command: "npm run dev"
        health_check:
          type: "regex"
          pattern: "ready in \\d+ms"
          interval: 2s
          timeout: 60s
`

func printUsage() {
	fmt.Println("Usage: devmux <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init                   Create a sample devmux.yaml in the current directory")
	fmt.Println("  start [config] [--ui]  Start the daemon (default config: devmux.yaml)")
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
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	if command == "help" || command == "--help" || command == "-h" {
		printUsage()
		return
	}

	if command == "init" {
		runInit()
		return
	}

	if command == "start" {
		runStart()
		return
	}

	if command == "ui" {
		ui := tui.NewStreamingTUI(protocol.GetSocketNetwork(), protocol.GetSocketPath())
		if err := ui.Run(); err != nil {
			if strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "refused") {
				fmt.Fprintf(os.Stderr, "Daemon is not running. Start it with: devmux start\n")
				os.Exit(1)
			}
			log.Fatalf("UI error: %v", err)
		}
		return
	}

	conn, err := net.Dial(protocol.GetSocketNetwork(), protocol.GetSocketPath())
	if err != nil {
		if command == "stop" {
			fmt.Println("Daemon is not running.")
			return
		}
		fmt.Fprintf(os.Stderr, "Daemon is not running. Start it with: devmux start\n")
		os.Exit(1)
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

// runInit creates a sample devmux.yaml in the current directory.
func runInit() {
	if _, err := os.Stat(defaultConfigName); err == nil {
		fmt.Fprintf(os.Stderr, "%s already exists. Remove it first or edit it directly.\n", defaultConfigName)
		os.Exit(1)
	}

	if err := os.WriteFile(defaultConfigName, []byte(sampleConfig), 0o644); err != nil {
		log.Fatalf("Failed to write %s: %v", defaultConfigName, err)
	}
	fmt.Printf("Created %s — edit it to match your project, then run: devmux start\n", defaultConfigName)
}

// runStart validates config, finds the daemon binary, launches it, and waits for it to be ready.
func runStart() {
	configPath := defaultConfigName
	openUI := false
	for _, arg := range os.Args[2:] {
		if arg == "--ui" || arg == "-u" {
			openUI = true
		} else {
			configPath = arg
		}
	}

	// Check config exists and is valid before launching daemon
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Config file %q not found.\n", configPath)
		fmt.Fprintf(os.Stderr, "Create one with: devmux init\n")
		os.Exit(1)
	}

	if _, err := config.Load(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	daemonBin, err := findDaemonBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Resolve config to absolute path so daemon finds it regardless of cwd
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		log.Fatalf("Failed to resolve config path: %v", err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(daemonBin, absConfig)
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		os.Exit(1)
	}

	// Wait for daemon socket to appear
	if err := waitForDaemon(cmd, &stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Daemon failed to start: %v\n", err)
		if stderr.Len() > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", strings.TrimSpace(stderr.String()))
		}
		os.Exit(1)
	}

	fmt.Printf("Daemon started (PID %d)\n", cmd.Process.Pid)

	if openUI {
		ui := tui.NewStreamingTUI(protocol.GetSocketNetwork(), protocol.GetSocketPath())
		if err := ui.Run(); err != nil {
			log.Fatalf("UI error: %v", err)
		}
	}
}

// findDaemonBinary locates the devmuxd binary.
func findDaemonBinary() (string, error) {
	binaryName := "devmuxd"
	if runtime.GOOS == "windows" {
		binaryName = "devmuxd.exe"
	}

	// Check next to the running devmux binary
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), binaryName)
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}

	// Check PATH
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("devmuxd not found. Ensure it's installed or on your PATH")
}

// waitForDaemon polls the socket until the daemon is reachable or the process exits.
func waitForDaemon(cmd *exec.Cmd, stderr *bytes.Buffer) error {
	network := protocol.GetSocketNetwork()
	address := protocol.GetSocketPath()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			// Daemon exited before socket appeared
			if err != nil {
				return fmt.Errorf("process exited: %v", err)
			}
			return fmt.Errorf("process exited unexpectedly")
		case <-deadline:
			cmd.Process.Kill()
			return fmt.Errorf("timed out waiting for daemon to start")
		case <-ticker.C:
			conn, err := net.DialTimeout(network, address, 50*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		}
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
	conn, err := net.Dial(protocol.GetSocketNetwork(), protocol.GetSocketPath())
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
