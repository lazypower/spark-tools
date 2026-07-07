package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/progress"
	"github.com/lazypower/spark-tools/internal/version"
	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "hfetch: %v\n", err)
		os.Exit(1)
	}
}

// runPullFn indirects the bare-arg shorthand's call to runPull so a test can
// assert that cobra actually reaches the root RunE (rather than rejecting a
// bare repo id as an unknown subcommand) without performing a real download.
var runPullFn = runPull

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "hfetch",
		Short:   "HuggingFace client in pure Go",
		Long:    "hfetch — download, manage, and inspect GGUF models from HuggingFace Hub.",
		Version: version.Version,
		// Bare-arg shorthand: `hfetch org/model` → `hfetch pull org/model`
		// (interactive). ArbitraryArgs is required — without it cobra rejects the
		// bare repo id as an unknown subcommand before RunE ever runs. A single
		// arg that looks like a repo id (has a "/") is pulled with the default
		// profile; anything else keeps cobra's unknown-command behavior so typos
		// of real subcommands still get a helpful error.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				return cmd.Help()
			case len(args) == 1 && strings.Contains(args[0], "/"):
				return runPullFn(cmd, args[0], pullFlags{profile: defaultPullProfile})
			default:
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		searchCmd(),
		infoCmd(),
		filesCmd(),
		pullCmd(),
		verifyCmd(),
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
// formatSize delegates to the shared size formatter — the single authority in
// internal/progress. (The previous local copy was byte-identical; collapsed here
// to remove a duplicate authority. cmd/llm-run keeps a deliberately DIVERGENT
// copy with no KB tier — see docs/internal-extraction-map.md.)
func formatSize(bytes int64) string { return progress.FormatSize(bytes) }
