package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/prompts"
)

func promptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Manage benchmark prompt sets",
	}
	cmd.AddCommand(promptsListCmd(), promptsShowCmd())
	return cmd
}

func promptsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available prompt sets",
		RunE: func(cmd *cobra.Command, args []string) error {
			sets := prompts.BuiltinSets()

			fmt.Printf("Built-in prompt sets:\n\n")
			for _, s := range sets {
				fmt.Printf("  %-12s  %3d prompts  %s\n", s.Name, s.Count, s.Description)
			}

			return nil
		},
	}
}

func promptsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Preview prompts in a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			promptList, err := prompts.LoadBuiltin(name)
			if err != nil {
				return err
			}

			fmt.Printf("Prompt set: %s (%d prompts)\n\n", name, len(promptList))
			for i, p := range promptList {
				// Truncate long prompts for display
				display := p
				if len(display) > 120 {
					display = display[:117] + "..."
				}
				// Replace newlines for compact display
				display = strings.ReplaceAll(display, "\n", " ")
				fmt.Printf("  %2d. %s\n", i+1, display)
			}

			return nil
		},
	}
}
