package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-bench: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "llm-bench",
		Short: "LLM benchmark suite",
		Long:  "llm-bench — declarative benchmarking for local LLMs on DGX Spark hardware.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		runCmd(),
		quickCmd(),
		resultsCmd(),
		compareCmd(),
		reportCmd(),
		promptsCmd(),
		initCmd(),
	)

	return root
}
