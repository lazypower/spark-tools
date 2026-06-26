package main

import (
	"context"
	"fmt"
	"path"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/quant"
)

func infoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <model_id>",
		Short: "Show detailed model information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			showFiles, _ := cmd.Flags().GetBool("files")
			remote, _ := cmd.Flags().GetBool("remote")

			var client *api.Client
			if remote {
				// Skip cache — create client without cache dir.
				tok := config.ResolveToken(resolveToken(cmd))
				client = api.NewClient(api.WithToken(tok.Token))
			} else {
				client = newAPIClient(cmd)
			}

			model, err := client.GetModel(context.Background(), args[0])
			if err != nil {
				return err
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			fmt.Printf("\n  %s\n", headerStyle.Render(model.ID))
			if model.Author != "" {
				fmt.Printf("  Author: %s\n", model.Author)
			}
			fmt.Printf("  Downloads: %d\n", model.Downloads)
			if len(model.Tags) > 0 {
				fmt.Printf("  Tags: %s\n", dimStyle.Render(fmt.Sprintf("%v", model.Tags)))
			}

			// Quant format — read from config without pulling the weights.
			if qi := fetchQuantInfo(context.Background(), client, args[0]); qi != nil {
				fmt.Printf("  Quantization: %s\n", qi)
			}

			if showFiles {
				files, err := client.ListFiles(context.Background(), args[0])
				if err != nil {
					return err
				}
				fmt.Println()
				fmt.Printf("  Files (%d):\n", len(files))
				for _, f := range files {
					quant := gguf.ParseQuantFromFilename(f.Filename)
					size := f.Size
					if f.LFS != nil {
						size = f.LFS.Size
					}
					if quant != "" {
						fmt.Printf("    %-50s %s  %s\n", f.Filename, quant, formatSize(size))
					} else {
						fmt.Printf("    %-50s %s\n", f.Filename, formatSize(size))
					}
				}
			}
			fmt.Println()
			return nil
		},
	}

	cmd.Flags().Bool("files", false, "Include file listing")
	cmd.Flags().Bool("remote", false, "Force fresh API fetch (skip cache)")
	tokenFlag(cmd)
	return cmd
}

// fetchQuantInfo reads a model's quantization format from its config files
// without downloading the weights. Returns nil when the model is
// unquantized or its config is unreadable.
func fetchQuantInfo(ctx context.Context, client *api.Client, modelID string) *quant.Info {
	files, err := client.ListFiles(ctx, modelID)
	if err != nil {
		return nil
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[path.Base(f.Filename)] = true
	}
	if !present["config.json"] {
		return nil
	}
	fetch := func(name string) []byte {
		if !present[name] {
			return nil
		}
		data, err := client.FetchFileRange(ctx, modelID, name, 0, 1<<20)
		if err != nil {
			return nil
		}
		return data
	}
	return quant.Parse(fetch("config.json"), fetch("hf_quant_config.json"), fetch("quantize_config.json"))
}
