package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/version"
	"github.com/lazypower/spark-tools/pkg/llmtidy"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-tidy: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		manifestPath string
		ollamaHost   string
	)
	root := &cobra.Command{
		Use:           "llm-tidy",
		Short:         "Declarative model inventory management",
		Long:          "llm-tidy — reconcile a YAML manifest of desired models against the local Ollama and GGUF backends.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&manifestPath, "manifest", "", "explicit manifest path (overrides $LLM_TIDY_MANIFEST)")
	root.PersistentFlags().StringVar(&ollamaHost, "ollama-host", "", "explicit Ollama base URL (overrides $OLLAMA_HOST)")

	root.AddCommand(
		statusCmd(),
		pruneCmd(),
		syncCmd(),
		promoteCmd(),
		demoteCmd(),
		initCmd(),
	)
	return root
}

// newTidy builds a Tidy honoring the persistent flags on cmd.
func newTidy(cmd *cobra.Command) (*llmtidy.Tidy, error) {
	return newTidyWithOverride(cmd, "")
}

// newTidyWithOverride is the same as newTidy except an explicit manifest
// override (used by `init --output`) takes precedence over the persistent
// --manifest flag.
func newTidyWithOverride(cmd *cobra.Command, manifestOverride string) (*llmtidy.Tidy, error) {
	manifestPath, _ := cmd.Flags().GetString("manifest")
	ollamaHost, _ := cmd.Flags().GetString("ollama-host")

	var opts []llmtidy.Option
	if manifestOverride != "" {
		opts = append(opts, llmtidy.WithManifestPath(manifestOverride))
	} else if manifestPath != "" {
		opts = append(opts, llmtidy.WithManifestPath(manifestPath))
	}
	if ollamaHost != "" {
		opts = append(opts, llmtidy.WithOllamaHost(ollamaHost))
	}
	return llmtidy.New(opts...)
}
