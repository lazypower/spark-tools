package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-run: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "llm-run",
		Short:   "llama.cpp wrapper for humans",
		Long:    "llm-run — run LLMs locally with smart defaults, model resolution via hfetch, and persistent profiles.",
		Version: version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		chatCmd(),
		serveCmd(),
		runCmd(),
		profileCmd(),
		aliasCmd(),
		modelsCmd(),
		hwCmd(),
		explainCmd(),
		rawCmd(),
	)

	return root
}
