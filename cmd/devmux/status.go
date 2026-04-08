package main

import (
	"fmt"
	"log"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:     "status [name]",
	Aliases: []string{"ps"},
	Short:   "Show process status (all or specific)",
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		wait, _ := cmd.Flags().GetBool("wait")
		if wait && name == "" {
			log.Fatal("--wait requires a process name")
		}
		sendCommand(protocol.Request{Command: "status", Name: name})

		if wait {
			fmt.Printf("Waiting for %s to become healthy...\n", name)
			if err := waitForHealthyStatus(name); err != nil {
				log.Fatalf("Failed waiting for healthy status: %v", err)
			}
			fmt.Printf("%s is now healthy\n", name)
		}
	},
}

func init() {
	statusCmd.Flags().BoolP("wait", "w", false, "Wait for the process to become healthy")
}
