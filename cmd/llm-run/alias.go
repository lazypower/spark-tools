package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func aliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage model aliases",
	}

	cmd.AddCommand(
		aliasSetCmd(),
		aliasRmCmd(),
		aliasListCmd(),
	)

	return cmd
}

func aliasSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <name> <ref>",
		Short: "Create or update a model alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			if err := resolver.SetAlias(dirs.Config, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Alias %q → %s\n", args[0], args[1])
			return nil
		},
	}
}

func aliasRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a model alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			if err := resolver.RemoveAlias(dirs.Config, args[0]); err != nil {
				return err
			}
			fmt.Printf("Alias %q removed.\n", args[0])
			return nil
		},
	}
}

func aliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all model aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			aliases, err := resolver.ListAliases(dirs.Config)
			if err != nil {
				return err
			}

			if len(aliases) == 0 {
				fmt.Println("No aliases defined.")
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			fmt.Printf("\n  %s\n\n", headerStyle.Render("Aliases"))
			for name, ref := range aliases {
				fmt.Printf("  %-20s %s\n", headerStyle.Render(name), dimStyle.Render(ref))
			}
			fmt.Println()
			return nil
		},
	}
}
