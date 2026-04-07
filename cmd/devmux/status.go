package main

import (
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
		sendCommand(protocol.Request{Command: "status", Name: name})
	},
}
