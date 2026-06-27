package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmserve"
)

func livenessCmd() *cobra.Command {
	var protectedOnly bool
	cmd := &cobra.Command{
		Use:   "liveness [name]",
		Short: "Report which model artifacts are protected from eviction (derived live)",
		Long: "Answer 'is this artifact protected from eviction?' — the authority llm-tidy will\n" +
			"consult before pruning. Derived live from running managed containers + desired\n" +
			"manifests; fail-closed (docker unreachable / unmappable ⇒ protected). No daemon.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, _, _ := dirs()
			lv := llmserve.NewLiveness(stateDir)
			out := cmd.OutOrStdout()
			ctx := context.Background()

			if protectedOnly {
				keys, all := lv.ProtectedArtifacts(ctx)
				if all {
					fmt.Fprintln(out, "ALL (fail-closed: liveness could not be fully determined)")
					return nil
				}
				for _, k := range keys {
					fmt.Fprintln(out, k)
				}
				return nil
			}

			if len(args) == 1 {
				il, err := lv.Instance(ctx, args[0])
				if err != nil {
					return err
				}
				verdict := "evictable"
				if il.Protected {
					verdict = "protected"
				}
				fmt.Fprintf(out, "%-20s %-10s %s\n", il.Name, verdict, il.Reason)
				return nil
			}

			keys, all := lv.ProtectedArtifacts(ctx)
			if all {
				fmt.Fprintln(out, "protected: ALL (fail-closed: liveness could not be fully determined)")
				return nil
			}
			if len(keys) == 0 {
				fmt.Fprintln(out, "no protected artifacts (nothing managed is running or desired)")
				return nil
			}
			fmt.Fprintln(out, "protected artifacts:")
			for _, k := range keys {
				fmt.Fprintf(out, "  %s\n", k)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&protectedOnly, "protected-artifacts", false, "print only the protected artifact paths (one per line, machine-readable)")
	return cmd
}
