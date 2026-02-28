package main

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/profiles"
)

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage saved profiles",
	}

	cmd.AddCommand(
		profileListCmd(),
		profileShowCmd(),
		profileSaveCmd(),
		profileEditCmd(),
		profileRmCmd(),
	)

	return cmd
}

func profileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			store := profiles.NewProfileStore(dirs.Config)

			list, err := store.List()
			if err != nil {
				return err
			}

			if len(list) == 0 {
				fmt.Println("No profiles found.")
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			fmt.Printf("\n  %s\n\n", headerStyle.Render("Profiles"))
			for _, p := range list {
				model := p.Config.ModelRef
				if model == "" {
					model = "(no model)"
				}
				fmt.Printf("  %-15s %s  %s\n",
					headerStyle.Render(p.Name),
					dimStyle.Render(p.Description),
					dimStyle.Render(model))
			}
			fmt.Println()
			return nil
		},
	}
}

func profileShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show profile details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			store := profiles.NewProfileStore(dirs.Config)

			p, err := store.Get(args[0])
			if err != nil {
				return err
			}

			data, _ := json.MarshalIndent(p, "  ", "  ")
			fmt.Printf("\n  %s\n\n", string(data))
			return nil
		},
	}
}

func profileSaveCmd() *cobra.Command {
	var (
		model   string
		ctxSize int
		temp    float64
		desc    string
		system  string
	)

	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save current config as profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			store := profiles.NewProfileStore(dirs.Config)

			p := profiles.Profile{
				Name:        args[0],
				Description: desc,
				Config: engine.RunConfig{
					ModelRef:    model,
					ContextSize: ctxSize,
					Temperature: temp,
					SystemPrompt: system,
				},
			}

			if err := store.Save(p); err != nil {
				return err
			}
			fmt.Printf("Profile %q saved.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Model reference")
	cmd.Flags().IntVar(&ctxSize, "ctx", 0, "Context size")
	cmd.Flags().Float64Var(&temp, "temp", 0, "Temperature")
	cmd.Flags().StringVar(&desc, "description", "", "Profile description")
	cmd.Flags().StringVar(&system, "system", "", "System prompt")

	return cmd
}

func profileEditCmd() *cobra.Command {
	var (
		model   string
		ctxSize int
		temp    float64
		desc    string
		system  string
	)

	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a profile's configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			store := profiles.NewProfileStore(dirs.Config)

			p, err := store.Get(args[0])
			if err != nil {
				return err
			}

			// Apply flag overrides to existing profile.
			if cmd.Flags().Changed("model") {
				p.Config.ModelRef = model
			}
			if cmd.Flags().Changed("ctx") {
				p.Config.ContextSize = ctxSize
			}
			if cmd.Flags().Changed("temp") {
				p.Config.Temperature = temp
			}
			if cmd.Flags().Changed("description") {
				p.Description = desc
			}
			if cmd.Flags().Changed("system") {
				p.Config.SystemPrompt = system
			}

			if err := store.Save(*p); err != nil {
				return err
			}
			fmt.Printf("Profile %q updated.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Model reference")
	cmd.Flags().IntVar(&ctxSize, "ctx", 0, "Context size")
	cmd.Flags().Float64Var(&temp, "temp", 0, "Temperature")
	cmd.Flags().StringVar(&desc, "description", "", "Profile description")
	cmd.Flags().StringVar(&system, "system", "", "System prompt")

	return cmd
}

func profileRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			store := profiles.NewProfileStore(dirs.Config)

			if err := store.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Profile %q deleted.\n", args[0])
			return nil
		},
	}
}
