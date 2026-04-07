package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devmux/internal/config"
	"devmux/internal/protocol"
	"devmux/internal/tui"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start [config]",
	Short: "Start the daemon",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		configPath := defaultConfigName
		if len(args) > 0 {
			configPath = args[0]
		}
		ui, _ := cmd.Flags().GetBool("ui")
		runStart(configPath, ui)
	},
}

func init() {
	startCmd.Flags().BoolP("ui", "u", false, "Open the TUI after starting")
}

func runStart(configPath string, openUI bool) {
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

func findDaemonBinary() (string, error) {
	binaryName := "devmuxd"
	if runtime.GOOS == "windows" {
		binaryName = "devmuxd.exe"
	}

	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), binaryName)
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("devmuxd not found. Ensure it's installed or on your PATH")
}

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
