package main

import (
	"fmt"
	"os"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump current running config as YAML to stdout",
	Run: func(cmd *cobra.Command, args []string) {
		resp := sendCommand(protocol.Request{Command: "dump"})
		if resp.Status == "ok" {
			fmt.Print(resp.Message)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
			os.Exit(1)
		}
	},
}
