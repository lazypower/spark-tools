package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show/edit configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Show current configuration.
			dirs := config.Dirs()
			fmt.Printf("\n  Config dir: %s\n", dirs.Config)
			fmt.Printf("  Data dir:   %s\n", dirs.Data)
			fmt.Printf("  Cache dir:  %s\n", dirs.Cache)

			prefs, _ := loadPrefs()
			if len(prefs) > 0 {
				fmt.Println("\n  Settings:")
				for k, v := range prefs {
					fmt.Printf("    %s = %v\n", k, v)
				}
			}
			fmt.Println()
			return nil
		},
	}

	cmd.AddCommand(configSetCmd(), configGetCmd())
	return cmd
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			prefs, err := loadPrefs()
			if err != nil {
				prefs = make(map[string]any)
			}
			prefs[key] = value

			if err := savePrefs(prefs); err != nil {
				return err
			}
			fmt.Printf("  Set %s = %s\n", key, value)
			return nil
		},
	}
}

func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefs, err := loadPrefs()
			if err != nil {
				return fmt.Errorf("no configuration found")
			}
			v, ok := prefs[args[0]]
			if !ok {
				return fmt.Errorf("key %q not set", args[0])
			}
			fmt.Println(v)
			return nil
		},
	}
}

func prefsPath() string {
	dirs := config.Dirs()
	return filepath.Join(dirs.Config, "config.json")
}

func loadPrefs() (map[string]any, error) {
	data, err := os.ReadFile(prefsPath())
	if err != nil {
		return nil, err
	}
	var prefs map[string]any
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}
	return prefs, nil
}

func savePrefs(prefs map[string]any) error {
	dirs := config.Dirs()
	if err := os.MkdirAll(dirs.Config, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(prefsPath(), data, 0644)
}
