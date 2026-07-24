package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name|tab>...",
	Short: "Restart one or more processes, or every process in a tab",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wait, _ := cmd.Flags().GetBool("wait")

		names, err := resolveTargets(args)
		if err != nil {
			log.Fatalf("%v", err)
		}

		var failed []string
		for _, name := range names {
			resp := sendCommand(protocol.Request{Command: "restart", Name: name})
			if resp.Status != "ok" {
				failed = append(failed, name)
				continue
			}

			if wait {
				fmt.Printf("Waiting for %s to become healthy...\n", name)
				if err := waitForHealthyStatus(name); err != nil {
					fmt.Printf("Failed waiting for %s: %v\n", name, err)
					failed = append(failed, name)
					continue
				}
				fmt.Printf("%s is now healthy\n", name)
			}
		}

		if len(names) > 1 {
			fmt.Printf("Restarted %d/%d process(es)\n", len(names)-len(failed), len(names))
		}
		if len(failed) > 0 {
			log.Fatalf("Failed to restart: %s", strings.Join(failed, ", "))
		}
	},
}

func init() {
	restartCmd.Flags().BoolP("wait", "w", false, "Wait for the process to become healthy")
}

// resolveTargets expands the given tokens into a de-duplicated, ordered list of
// pane names. Each token is resolved pane-first: a token matching a pane name
// refers to that single pane; otherwise a token matching a tab name expands to
// all panes in that tab. Unknown tokens are reported as an error.
func resolveTargets(tokens []string) ([]string, error) {
	layout, err := fetchLayout()
	if err != nil {
		return nil, err
	}

	panes := map[string]bool{}
	tabPanes := map[string][]string{}
	for _, tab := range layout.Tabs {
		for _, pane := range tab.Panes {
			panes[pane.Name] = true
			tabPanes[tab.Name] = append(tabPanes[tab.Name], pane.Name)
		}
	}

	var result []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	var unknown []string
	for _, token := range tokens {
		switch {
		case panes[token]:
			add(token)
		case tabPanes[token] != nil:
			for _, p := range tabPanes[token] {
				add(p)
			}
		default:
			unknown = append(unknown, token)
		}
	}

	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown process or tab: %s", strings.Join(unknown, ", "))
	}
	return result, nil
}

// fetchLayout retrieves the current tab/pane layout from the daemon without
// printing anything to stdout.
func fetchLayout() (*protocol.Layout, error) {
	conn, err := net.Dial(protocol.GetSocketNetwork(), protocol.GetSocketPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Daemon is not running. Start it with: devmux start\n")
		os.Exit(1)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(protocol.Request{Command: "layout"}); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.Layout == nil {
		return nil, fmt.Errorf("daemon returned no layout")
	}
	return resp.Layout, nil
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
