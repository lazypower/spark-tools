package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/gguf"
)

func filesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files <model_id>",
		Short: "List files in a model repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			quantFilter, _ := cmd.Flags().GetString("quant")
			asJSON, _ := cmd.Flags().GetBool("json")

			client := newAPIClient(cmd)

			files, err := client.ListFiles(context.Background(), args[0])
			if err != nil {
				return err
			}

			// The JSON form is the repo tree llm-serve's completeness gate
			// consumes (`llm-serve emit --repo-tree`), so it emits the WHOLE
			// tree and applies none of the filters below. Those exist to make
			// the human table readable -- the default one hides everything that
			// is not GGUF.
			//
			// Measured, because the failure direction is not the obvious one:
			// the gate validates the TREE for serve-readiness, not just the
			// tree against the disk, so a filtered tree fails CLOSED rather
			// than passing a bad artifact. Feeding a GGUF-filtered (empty) tree
			// for a safetensors repo produced
			//
			//   *.safetensors: no safetensors weights and no index in repo
			//   config.json: required file not in repo
			//   tokenizer: no tokenizer file in repo
			//
			// against a repo that has all three. So the hazard is not a silent
			// pass, it is a confident and completely wrong REJECTION that sends
			// the operator looking for missing files that are present. Either
			// way the gate needs the real tree; it just fails the safer way.
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(files)
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
	cmd.Flags().Bool("json", false, "emit the full repo tree as JSON, for `llm-serve emit --repo-tree` (unfiltered: the completeness gate needs every file)")
	cmd.Flags().String("min-size", "", "Minimum file size")
	cmd.Flags().String("max-size", "", "Maximum file size")
	tokenFlag(cmd)
	return cmd
}
