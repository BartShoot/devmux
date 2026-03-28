package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"devmux/internal/config"
	"devmux/internal/daemon"
)

func main() {
	configPath := "devmux.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	pm := daemon.NewProcessManager()
	ctx := context.Background()
	go pm.RunHealthChecks(ctx)

	fmt.Printf("Successfully loaded configuration from %s\n", configPath)
	for _, tab := range cfg.Tabs {
		for _, pane := range tab.Panes {
			fmt.Printf("Starting process: %s\n", pane.Name)
			if err := pm.StartProcess(pane.Name, pane.Command, pane.HealthCheck); err != nil {
				log.Printf("Failed to start process %s: %v", pane.Name, err)
			}
		}
	}

	server := daemon.NewServer("devmux.sock", pm)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
