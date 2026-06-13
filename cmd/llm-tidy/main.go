package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-tidy: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "llm-tidy",
		Short:         "Declarative model inventory management",
		Long:          "llm-tidy — reconcile a YAML manifest of desired models against the local Ollama and GGUF backends.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return root
}
