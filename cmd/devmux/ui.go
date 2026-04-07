package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"devmux/internal/protocol"
	"devmux/internal/tui"

	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the TUI",
	Run: func(cmd *cobra.Command, args []string) {
		ui := tui.NewStreamingTUI(protocol.GetSocketNetwork(), protocol.GetSocketPath())
		if err := ui.Run(); err != nil {
			if strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "refused") {
				fmt.Fprintf(os.Stderr, "Daemon is not running. Start it with: devmux start\n")
				os.Exit(1)
			}
			log.Fatalf("UI error: %v", err)
		}
	},
}
