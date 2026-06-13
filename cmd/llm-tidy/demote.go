package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func demoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demote <model>",
		Short: "Remove a model from the manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tidy, err := newTidy(cmd)
			if err != nil {
				return err
			}
			if err := tidy.Demote(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Demoted %q from manifest at %s\n", args[0], tidy.ManifestPath())
			return nil
		},
	}
	return cmd
}
