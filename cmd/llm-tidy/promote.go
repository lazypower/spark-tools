package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func promoteCmd() *cobra.Command {
	var backend string
	cmd := &cobra.Command{
		Use:   "promote <model>",
		Short: "Add a model to the manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := resolveBackend(backend)
			if err != nil {
				return err
			}
			tidy, err := newTidy(cmd)
			if err != nil {
				return err
			}
			if err := tidy.Promote(cmd.Context(), args[0], b); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Promoted %q to manifest at %s\n", args[0], tidy.ManifestPath())
			return nil
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "required if model name is ambiguous (ollama|gguf)")
	return cmd
}
