package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
)

func filesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files <model_id>",
		Short: "List files in a model repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			quantFilter, _ := cmd.Flags().GetString("quant")

			client := newAPIClient(cmd)

			files, err := client.ListFiles(context.Background(), args[0])
			if err != nil {
				return err
			}

			headerStyle := lipgloss.NewStyle().Bold(true)

			fmt.Printf("\n  %s\n\n", headerStyle.Render(args[0]))

			// Build FileInfo list for filtering.
			var infos []gguf.FileInfo
			for _, f := range files {
				size := f.Size
				if f.LFS != nil {
					size = f.LFS.Size
				}
				quant := gguf.ParseQuantFromFilename(f.Filename)
				bpw := gguf.QuantBitsPerWeight[quant]
				infos = append(infos, gguf.FileInfo{
					Filename:      f.Filename,
					Size:          size,
					Quantization:  quant,
					BitsPerWeight: bpw,
				})
			}

			// Default: show only GGUF files if any exist.
			ggufFiles := gguf.FilterGGUF(infos)
			if len(ggufFiles) > 0 {
				infos = ggufFiles
			}

			// Apply quant filter.
			if quantFilter != "" {
				infos = gguf.FilterByQuant(infos, quantFilter)
			}

			// Group by quant to collapse split shards.
			groups := gguf.GroupByQuant(infos)

			fmt.Printf("  %-12s %-10s %-10s %-12s %-8s %s\n", "Quant", "Size", "Shards", "Bits/Weight", "Fit", "")
			fmt.Printf("  %s\n", lipgloss.NewStyle().Faint(true).Render(
				"────────────────────────────────────────────────────────────────────────────",
			))

			for _, g := range groups {
				bpw := ""
				if g.BitsPerWeight > 0 {
					bpw = fmt.Sprintf("%.2f", g.BitsPerWeight)
				}
				quant := g.Quantization
				if quant == "" {
					quant = "—"
				}

				shards := ""
				if g.ShardCount > 1 {
					shards = fmt.Sprintf("%d files", g.ShardCount)
				}

				fit := gguf.EstimateFit(g.TotalSize, nil, 0)
				fitLabel := fit.FitLabel()

				qualLabel := ""
				if ql := gguf.QuantQualityLabel(g.Quantization); ql != "" {
					qualLabel = ql
				}

				fmt.Printf("  %-12s %-10s %-10s %-12s %-8s %s\n",
					quant, formatSize(g.TotalSize), shards, bpw, fitLabel, qualLabel)
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().String("quant", "", "Filter by quantization type")
	cmd.Flags().String("min-size", "", "Minimum file size")
	cmd.Flags().String("max-size", "", "Maximum file size")
	tokenFlag(cmd)
	return cmd
}
