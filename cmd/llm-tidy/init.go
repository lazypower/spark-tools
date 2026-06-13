package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

func initCmd() *cobra.Command {
	var (
		backend string
		output  string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a manifest from current inventory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := resolveBackend(backend)
			if err != nil {
				return err
			}
			tidy, err := newTidyWithOverride(cmd, output)
			if err != nil {
				return err
			}
			m, err := tidy.Init(cmd.Context())
			if err != nil {
				return err
			}

			ol, gg := countSpecs(m, b)
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Scanned %d Ollama models and %d GGUF models.\n", ol, gg)
			fmt.Fprintf(w, "Wrote manifest to %s\n", tidy.ManifestPath())
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Edit the manifest to remove models you don't want to keep,")
			fmt.Fprintln(w, "then run 'llm-tidy prune' to clean up.")
			return nil
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "report counts for one backend only (ollama|gguf)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to a specific manifest path")
	return cmd
}

func countSpecs(m *llmtidy.Manifest, b inventory.ModelBackend) (int, int) {
	ol, gg := len(m.Ollama), len(m.GGUF)
	if b == inventory.BackendOllama {
		gg = 0
	}
	if b == inventory.BackendGGUF {
		ol = 0
	}
	return ol, gg
}
