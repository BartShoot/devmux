package main

import (
	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var presetsCmd = &cobra.Command{
	Use:   "presets <name>",
	Short: "List or add command presets",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		add, _ := cmd.Flags().GetString("add")
		label, _ := cmd.Flags().GetString("label")

		sendCommand(protocol.Request{
			Command:     "presets",
			Name:        name,
			AddPreset:   add,
			PresetLabel: label,
		})
	},
}

func init() {
	presetsCmd.Flags().String("add", "", "Add a new command preset")
	presetsCmd.Flags().String("label", "", "Label for the added preset")
}
