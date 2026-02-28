package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with HuggingFace",
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenValue, _ := cmd.Flags().GetString("token")

			headerStyle := lipgloss.NewStyle().Bold(true)
			fmt.Printf("\n  %s\n", headerStyle.Render("HuggingFace Login"))
			fmt.Println("  ─────────────────")

			if tokenValue == "" {
				fmt.Println("  1. Go to https://huggingface.co/settings/tokens")
				fmt.Println("  2. Create a token with \"Read\" access")
				fmt.Println("  3. Paste it below.")
				fmt.Println()

				var input string
				form := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("Token").
							EchoMode(huh.EchoModePassword).
							Value(&input),
					),
				)
				if err := form.Run(); err != nil {
					return err
				}
				tokenValue = input
			}

			if tokenValue == "" {
				return fmt.Errorf("no token provided")
			}

			// Validate the token.
			fmt.Print("  Verifying... ")
			client := api.NewClient(api.WithToken(tokenValue))
			info, err := client.WhoAmI(context.Background())
			if err != nil {
				fmt.Fprintln(os.Stderr, "✗")
				return err
			}
			fmt.Printf("✓ Authenticated as %q\n", info.Username)

			// Store the token.
			if err := config.StoreToken(tokenValue); err != nil {
				return fmt.Errorf("saving token: %w", err)
			}

			dirs := config.Dirs()
			fmt.Printf("\n  Token saved to %s/token.json\n", dirs.Config)
			fmt.Println("  This token is shared with llm-run and llm-bench — no need to log in again.")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().String("token", "", "Provide token non-interactively")
	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored authentication token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.ClearToken(); err != nil {
				return err
			}
			fmt.Println("  Token removed.")
			return nil
		},
	}
}

func whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current auth status and token source",
		RunE: func(cmd *cobra.Command, args []string) error {
			tok := config.ResolveToken("")

			if tok.Source == "none" || tok.Token == "" {
				fmt.Println("\n  Not authenticated.")
				fmt.Println("  Run `hfetch login` to authenticate.")
				fmt.Println()
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)

			// Validate the token.
			client := api.NewClient(api.WithToken(tok.Token))
			info, err := client.WhoAmI(context.Background())
			if err != nil {
				fmt.Printf("\n  %s\n", headerStyle.Render("Auth Status"))
				fmt.Printf("  Token source:  %s\n", tokenSourceLabel(tok.Source))
				fmt.Printf("  Token prefix:  %s\n", redactToken(tok.Token))
				fmt.Printf("  Status:        invalid (%v)\n\n", err)
				return nil
			}

			fmt.Printf("\n  %s\n", headerStyle.Render("Auth Status"))
			fmt.Printf("  Authenticated as: %s\n", info.Username)
			fmt.Printf("  Token source:     %s\n", tokenSourceLabel(tok.Source))
			fmt.Printf("  Token prefix:     %s\n", redactToken(tok.Token))
			fmt.Println()

			return nil
		},
	}
}
