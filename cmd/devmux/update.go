package main

import (
	"log"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update process command or preset",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		newCmd, _ := cmd.Flags().GetString("cmd")
		preset, _ := cmd.Flags().GetString("preset")

		if newCmd == "" && preset == "" {
			log.Fatal("Usage: devmux update <name> --cmd \"command\" | --preset <label|number>")
		}

		sendCommand(protocol.Request{
			Command:    "update",
			Name:       name,
			NewCommand: newCmd,
			Preset:     preset,
		})
	},
}

func init() {
	updateCmd.Flags().String("cmd", "", "New command to run")
	updateCmd.Flags().String("preset", "", "Switch to preset by label or number")
}
