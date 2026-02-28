package main

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded models",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			showPath, _ := cmd.Flags().GetBool("path")

			dirs := config.Dirs()
			reg := registry.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			models := reg.List()

			if jsonOutput {
				data, err := json.MarshalIndent(models, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			if len(models) == 0 {
				fmt.Println("  No models downloaded. Use `hfetch pull` to download a model.")
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			for _, m := range models {
				fmt.Printf("\n  %s\n", headerStyle.Render(m.ID))
				for _, f := range m.Files {
					status := "✓"
					if !f.Complete {
						status = "…"
					}
					line := fmt.Sprintf("    %s %-10s %s", status, f.Quantization, formatSize(f.Size))
					if showPath {
						line += "  " + dimStyle.Render(f.LocalPath)
					}
					fmt.Println(line)
				}
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().Bool("json", false, "JSON output for scripting")
	cmd.Flags().Bool("path", false, "Show full file paths")
	return cmd
}

func pathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <model_id> [filename]",
		Short: "Print the local path to a downloaded model file",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			reg := registry.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			filename := ""
			if len(args) > 1 {
				filename = args[1]
			}

			path := reg.Path(args[0], filename)
			if path == "" {
				return fmt.Errorf("model %q not found locally", args[0])
			}

			fmt.Println(path)
			return nil
		},
	}
}
