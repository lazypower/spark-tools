package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "hfetch: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hfetch",
		Short: "HuggingFace client in pure Go",
		Long:  "hfetch — download, manage, and inspect GGUF models from HuggingFace Hub.",
		// Bare-arg shorthand: hfetch org/model → hfetch pull org/model (interactive)
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && strings.Contains(args[0], "/") {
				return runPull(cmd, args[0], pullFlags{})
			}
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		searchCmd(),
		infoCmd(),
		filesCmd(),
		pullCmd(),
		listCmd(),
		pathCmd(),
		rmCmd(),
		gcCmd(),
		loginCmd(),
		logoutCmd(),
		whoamiCmd(),
		configCmd(),
		ollamaImportCmd(),
	)

	return root
}

func tokenFlag(cmd *cobra.Command) {
	cmd.Flags().String("token", "", "Override HuggingFace API token for this invocation")
}

func resolveToken(cmd *cobra.Command) string {
	tok, _ := cmd.Flags().GetString("token")
	return tok
}

// newAPIClient creates an API client with token and cache dir configured.
func newAPIClient(cmd *cobra.Command) *api.Client {
	tok := config.ResolveToken(resolveToken(cmd))
	dirs := config.Dirs()
	return api.NewClient(
		api.WithToken(tok.Token),
		api.WithCacheDir(dirs.Cache),
	)
}

// formatSize formats bytes as a human-readable string.
func formatSize(bytes int64) string {
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
		kb = 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
