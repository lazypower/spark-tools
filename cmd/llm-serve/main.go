// Command llm-serve is the vLLM contract engine: it resolves a serve request
// into a validated vLLM launch spec and emits it. v1 is emit-only — it does not
// launch, supervise, or own anything at runtime (that is v2).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-serve: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "llm-serve",
		Short:         "vLLM contract engine — resolve a serve request to a validated launch spec",
		Long:          "llm-serve — turn {model + capabilities + ctx + hardware} into a validated, host-appropriate vLLM launch spec, rejecting footgun flag combinations. Emit-only: it prints the spec for you (or compose/quadlet) to run.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		emitCmd(), profilesCmd(), targetsCmd(),
		upCmd(), downCmd(), statusCmd(), recoverCmd(), forgetCmd(),
		livenessCmd(),
	)
	return root
}
