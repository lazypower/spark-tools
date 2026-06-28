package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmserve"
)

func profilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List the built-in arch profiles and their capability claims",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			for _, p := range llmserve.BuiltinProfiles() {
				fmt.Fprintf(out, "%s\n", p.Arch)
				if len(p.AltArch) > 0 {
					fmt.Fprintf(out, "  also: %s\n", strings.Join(p.AltArch, ", "))
				}
				fmt.Fprintf(out, "  authored against: %s\n", p.AuthoredAgainst.Canonical())
				var supported []string
				for _, c := range p.Claims {
					if c.Supported {
						supported = append(supported, string(c.Capability)+" ("+string(c.Status)+")")
					}
				}
				fmt.Fprintf(out, "  capabilities: %s\n", strings.Join(supported, ", "))
			}
			return nil
		},
	}
}

func targetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets",
		Short: "List the supported render targets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, t := range llmserve.Targets() {
				fmt.Fprintln(cmd.OutOrStdout(), t)
			}
			return nil
		},
	}
}
