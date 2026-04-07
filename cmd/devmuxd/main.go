package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"

	"devmux/internal/config"
	"devmux/internal/daemon"
	"devmux/internal/protocol"
)

func main() {
	verbose := flag.Bool("v", false, "verbose: print process output to stdout")
	flag.Parse()

	configPath := "devmux.yaml"
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// pprof endpoint for profiling
	go func() {
		log.Println("pprof listening on http://localhost:6060/debug/pprof/")
		http.ListenAndServe("localhost:6060", nil)
	}()

	pm := daemon.NewProcessManager()
	pm.SetVerbose(*verbose)
	ctx := context.Background()
	go pm.RunHealthChecks(ctx)

	fmt.Printf("Successfully loaded configuration from %s\n", configPath)

	// Build layout for TUI
	layout := &protocol.Layout{
		Tabs: make([]protocol.TabLayout, len(cfg.Tabs)),
	}

	for i := range cfg.Tabs {
		tab := &cfg.Tabs[i]
		layout.Tabs[i] = protocol.TabLayout{
			Name:   tab.Name,
			Layout: tab.Layout,
			Panes:  make([]protocol.PaneLayout, len(tab.Panes)),
		}

		for j := range tab.Panes {
			pane := &tab.Panes[j]
			layout.Tabs[i].Panes[j] = protocol.PaneLayout{Name: pane.Name}

			cwd := cfg.ResolveCwd(tab, pane)
			fmt.Printf("Starting process: %s (cwd: %s)\n", pane.Name, cwd)
			if err := pm.StartProcess(pane.Name, pane.Commands[0].Command, cwd, pane.HealthCheck, pane.Commands); err != nil {
				log.Printf("Failed to start process %s: %v", pane.Name, err)
			}
		}
	}

	server := daemon.NewServer("devmux.sock", pm, layout)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
