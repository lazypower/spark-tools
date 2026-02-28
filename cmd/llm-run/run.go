package main

import (
	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	var (
		profileName string
		prompt      string
		promptFile  string
		format      string
		maxTokens   int
	)

	cmd := &cobra.Command{
		Use:   "run [model]",
		Short: "Single prompt, non-interactive",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelRef, err := resolveModelArg(args, profileName)
			if err != nil {
				return err
			}
			return runInference(modelRef, inferenceFlags{
				profileName: profileName,
				prompt:      prompt,
				promptFile:  promptFile,
				format:      format,
				maxTokens:   maxTokens,
				interactive: false,
			})
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "Use a saved profile")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Prompt from file")
	cmd.Flags().StringVar(&format, "format", "", "Request output format (e.g. json)")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Maximum tokens to generate")

	return cmd
}
