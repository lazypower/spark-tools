package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

func searchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search HuggingFace for models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gguf, _ := cmd.Flags().GetBool("gguf")
			sort, _ := cmd.Flags().GetString("sort")
			limit, _ := cmd.Flags().GetInt("limit")

			client := newAPIClient(cmd)

			filter := ""
			if gguf {
				filter = "gguf"
			}

			models, err := client.Search(context.Background(), args[0], api.SearchOptions{
				Filter: filter,
				Sort:   sort,
				Limit:  limit,
			})
			if err != nil {
				return err
			}

			if len(models) == 0 {
				fmt.Println("No models found.")
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			for _, m := range models {
				fmt.Printf("  %s  %s\n",
					headerStyle.Render(m.ID),
					dimStyle.Render(fmt.Sprintf("↓ %d", m.Downloads)),
				)
			}

			return nil
		},
	}

	cmd.Flags().Bool("gguf", false, "Only show repos containing GGUF files")
	cmd.Flags().String("sort", "downloads", "Sort by: downloads, updated, trending")
	cmd.Flags().Int("limit", 20, "Max results")
	tokenFlag(cmd)
	return cmd
}
