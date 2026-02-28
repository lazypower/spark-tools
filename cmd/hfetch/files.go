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

			// Apply filters.
			if quantFilter != "" {
				infos = gguf.FilterByQuant(infos, quantFilter)
			} else {
				// Default: show only GGUF files if any exist.
				ggufFiles := gguf.FilterGGUF(infos)
				if len(ggufFiles) > 0 {
					infos = ggufFiles
				}
			}

			gguf.SortByQuality(infos)

			fmt.Printf("  %-45s %-10s %-10s %-12s %s\n", "File", "Quant", "Size", "Bits/Weight", "Fit")
			fmt.Printf("  %s\n", lipgloss.NewStyle().Faint(true).Render(
				"────────────────────────────────────────────────────────────────────────────────────────",
			))

			for _, f := range infos {
				bpw := ""
				if f.BitsPerWeight > 0 {
					bpw = fmt.Sprintf("%.2f", f.BitsPerWeight)
				}
				quant := f.Quantization
				if quant == "" {
					quant = "—"
				}

				// Fit estimation (best-effort with available metadata).
				fit := gguf.EstimateFit(f.Size, nil, 0)
				fitLabel := fit.FitLabel()

				fmt.Printf("  %-45s %-10s %-10s %-12s %s\n",
					f.Filename, quant, formatSize(f.Size), bpw, fitLabel)
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
