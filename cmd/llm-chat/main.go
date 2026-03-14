package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/tui"
	"github.com/lazypower/spark-tools/internal/version"
	"github.com/lazypower/spark-tools/pkg/llmrun/api"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-chat: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		apiKey string
		model  string
		system string
	)

	cmd := &cobra.Command{
		Use:     "llm-chat <endpoint>",
		Short:   "Chat TUI for any OpenAI-compatible endpoint",
		Long:    "llm-chat — connect to a remote or local LLM server and chat. No model management, no llama.cpp, just talk.",
		Version: version.Version,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := strings.TrimRight(args[0], "/")

			var opts []api.Option
			if apiKey != "" {
				opts = append(opts, api.WithAPIKey(apiKey))
			}
			client := api.NewClient(endpoint, opts...)

			cfg := tui.ChatConfig{
				Client:    client,
				ModelName: model,
				MultiLine: true,
			}

			if model == "" {
				cfg.ModelName = endpoint
			}

			var messages []api.Message
			if system != "" {
				messages = append(messages, api.Message{
					Role:    "system",
					Content: system,
				})
			}

			return tui.RunChat(cfg, messages...)
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for authenticated endpoints")
	cmd.Flags().StringVar(&model, "model", "", "Model name to display in header")
	cmd.Flags().StringVar(&system, "system", "", "System prompt")

	return cmd
}
