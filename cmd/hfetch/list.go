package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/modelstore"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded models",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			showPath, _ := cmd.Flags().GetBool("path")

			dirs := config.Dirs()
			reg := modelstore.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			models := reg.List()

			if jsonOutput {
				data, err := json.MarshalIndent(models, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			if len(models) == 0 {
				fmt.Println("  No models downloaded. Use `hfetch pull` to download a model.")
				return nil
			}

			headerStyle := lipgloss.NewStyle().Bold(true)
			dimStyle := lipgloss.NewStyle().Faint(true)

			for _, m := range models {
				fmt.Printf("\n  %s\n", headerStyle.Render(m.ID))
				for _, f := range m.Files {
					status := "✓"
					if !f.Complete {
						status = "…"
					}
					// Quantization identifies a GGUF file usefully, but it is
					// empty for everything else -- so a safetensors listing
					// rendered only a checkmark and a size, with no way to tell
					// which file each row was. Fall back to the filename.
					label := f.Quantization
					if label == "" {
						label = filepath.Base(f.LocalPath)
					}
					line := fmt.Sprintf("    %s %-14s %s", status, label, formatSize(f.Size))
					if showPath {
						line += "  " + dimStyle.Render(f.LocalPath)
					}
					fmt.Println(line)
				}
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().Bool("json", false, "JSON output for scripting")
	cmd.Flags().Bool("path", false, "Show full file paths")
	return cmd
}

func pathCmd() *cobra.Command {
	var dirOnly bool

	cmd := &cobra.Command{
		Use:   "path <model_id> [filename]",
		Short: "Print the local path to a downloaded model file",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			reg := modelstore.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			filename := ""
			if len(args) > 1 {
				filename = args[1]
			}

			path := reg.Path(args[0], filename)
			if path == "" {
				return fmt.Errorf("model %q not found locally", args[0])
			}

			// A serve-ready artifact is addressed by its DIRECTORY, not by one
			// file inside it: `llm-serve emit --model-dir` and vLLM both want
			// the folder. Without this the caller has to know that a bare path
			// returns whichever file sorts first -- config.json for a
			// safetensors pull -- and wrap the call in dirname.
			if dirOnly {
				fmt.Println(filepath.Dir(path))
				return nil
			}

			fmt.Println(path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dirOnly, "dir", false, "print the containing directory instead of a file path (what `llm-serve emit --model-dir` takes)")

	return cmd
}
