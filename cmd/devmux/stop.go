package main

import (
	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon",
	Run: func(cmd *cobra.Command, args []string) {
		sendCommand(protocol.Request{Command: "shutdown"})
	},
}
