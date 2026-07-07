package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/openaiapi"
	"github.com/lazypower/spark-tools/internal/tui"
	"github.com/lazypower/spark-tools/internal/version"
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

			var opts []openaiapi.Option
			if apiKey != "" {
				opts = append(opts, openaiapi.WithAPIKey(apiKey))
			}
			client := openaiapi.NewClient(endpoint, opts...)

			cfg := tui.ChatConfig{
				Client:    client,
				ModelName: resolveModel(cmd.Context(), client, model),
				MultiLine: true,
			}

			var messages []openaiapi.Message
			if system != "" {
				messages = append(messages, openaiapi.Message{
					Role:    "system",
					Content: system,
				})
			}

			return tui.RunChat(cfg, messages...)
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for authenticated endpoints")
	cmd.Flags().StringVar(&model, "model", "", "Model name to send and display (default: ask the server)")
	cmd.Flags().StringVar(&system, "system", "", "System prompt")

	return cmd
}

// resolveModel picks the model name to send in requests and show in the header:
// an explicit --model wins; otherwise the server's model when it advertises
// exactly one (/v1/models); otherwise empty, which is omitted from requests so
// the server uses its default. It NEVER returns the endpoint URL — llama-server
// ignores the model field but vLLM and gateways 404 on a bogus model.
//
// Discovery is bounded by a short timeout so a stalled /v1/models doesn't hang
// startup, and it refuses to guess when the server advertises MULTIPLE models —
// the first entry may be an embeddings/rerank model that would 400 a chat call —
// hinting the user to pass --model instead.
func resolveModel(ctx context.Context, client *openaiapi.Client, explicit string) string {
	if explicit != "" {
		return explicit
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.ListModels(probeCtx)
	if err != nil || resp == nil || len(resp.Data) == 0 {
		return "" // no advertised model → omit the field, server picks its default
	}
	if len(resp.Data) == 1 {
		return resp.Data[0].ID
	}
	fmt.Fprintf(os.Stderr, "llm-chat: server advertises %d models; pass --model to choose one\n", len(resp.Data))
	return ""
}
