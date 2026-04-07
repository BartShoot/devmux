package main

import (
	"fmt"
	"strings"

	"devmux/internal/protocol"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show process logs",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		tail, _ := cmd.Flags().GetInt("tail")
		grep, _ := cmd.Flags().GetString("grep")
		after, _ := cmd.Flags().GetInt("after")
		before, _ := cmd.Flags().GetInt("before")
		context, _ := cmd.Flags().GetInt("context")

		if context > 0 {
			after = context
			before = context
		}

		req := protocol.Request{
			Command: "logs",
			Name:    name,
			Tail:    tail,
		}

		resp := sendCommand(req)

		if resp.Status == "ok" && grep != "" {
			fmt.Print(grepLines(resp.Message, grep, before, after))
		} else if resp.Status == "ok" {
			fmt.Print(resp.Message)
		} else {
			fmt.Printf("%s: %s\n", resp.Status, resp.Message)
		}
	},
}

func init() {
	logsCmd.Flags().IntP("tail", "t", 0, "Show only last N lines")
	logsCmd.Flags().StringP("grep", "g", "", "Filter lines containing PATTERN")
	logsCmd.Flags().IntP("after", "A", 0, "Show N lines after each match")
	logsCmd.Flags().IntP("before", "B", 0, "Show N lines before each match")
	logsCmd.Flags().IntP("context", "C", 0, "Show N lines before and after each match")
}

func grepLines(text, pattern string, before, after int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var matches []int
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			matches = append(matches, i)
		}
	}

	if len(matches) == 0 {
		return ""
	}

	include := make([]bool, len(lines))
	for _, m := range matches {
		start := m - before
		if start < 0 {
			start = 0
		}
		end := m + after
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for i := start; i <= end; i++ {
			include[i] = true
		}
	}

	var result []string
	prevIncluded := false
	for i, line := range lines {
		if include[i] {
			if !prevIncluded && len(result) > 0 {
				result = append(result, "--")
			}
			result = append(result, line)
			prevIncluded = true
		} else {
			prevIncluded = false
		}
	}

	return strings.Join(result, "\n") + "\n"
}
