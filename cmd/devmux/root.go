package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devmux",
	Short: "devmux is a developer buddy for process management",
	Long:  `A missing link between AI agent automation and fragile process management.`,
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(uiCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(presetsCmd)
	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(logsCmd)
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
