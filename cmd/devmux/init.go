package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

const sampleConfig = `# devmux configuration
# See https://github.com/BartShoot/devmux for documentation.

# Global working directory (optional). Relative pane/tab cwds resolve against this.
# cwd: /home/user/projects

tabs:
  - name: "app"
    # layout: "split"  # "vertical" (default), "horizontal", or "split"
    panes:
      - name: "server"
        # Commands list: first is default. Use labels for quick switching.
        commands:
          - normal: "go run ./cmd/server"
          - debug: "go run ./cmd/server -debug"
        # cwd: "backend"  # relative to tab or global cwd
        health_check:
          type: "http"
          url: "http://localhost:8080/health"
          interval: 5s
          timeout: 30s

      - name: "worker"
        commands:
          - "go run ./cmd/worker"
        health_check:
          type: "tcp"
          address: "localhost:9090"
          interval: 5s
          timeout: 30s

  - name: "frontend"
    panes:
      - name: "dev-server"
        commands:
          - "npm run dev"
        health_check:
          type: "regex"
          pattern: "ready in \\d+ms"
          interval: 2s
          timeout: 60s
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a sample devmux.yaml in the current directory",
	Run: func(cmd *cobra.Command, args []string) {
		runInit()
	},
}

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
