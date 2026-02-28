package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
)

func modelsCmd() *cobra.Command {
	var (
		localOnly bool
		remote    bool
	)

	cmd := &cobra.Command{
		Use:   "models [query]",
		Short: "List available models",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if remote {
				query := ""
				if len(args) > 0 {
					query = args[0]
				}
				if query == "" {
					return fmt.Errorf("provide a search query with --remote, e.g.: llm-run models --remote qwen")
				}
				fmt.Printf("  Use 'hfetch search %s' to search HuggingFace models.\n", query)
				return nil
			}
			return listModels(localOnly)
		},
	}

	cmd.Flags().BoolVar(&localOnly, "local", false, "Only show locally available models")
	cmd.Flags().BoolVar(&remote, "remote", false, "Search HuggingFace (delegates to hfetch)")

	return cmd
}

type localModel struct {
	repo  string
	file  string
	quant string
	size  int64
}

func listModels(localOnly bool) error {
	hfDirs := hfconfig.Dirs()

	// Scan local hfetch data directory for downloaded models.
	models, err := scanLocalModels(hfDirs.Data)
	if err != nil {
		return fmt.Errorf("scanning local models: %w", err)
	}

	if len(models) == 0 {
		fmt.Println("No local models found.")
		fmt.Println("  Use 'hfetch pull <model>' to download a model.")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Faint(true)

	fmt.Printf("\n  %s\n\n", headerStyle.Render("Local Models"))

	for _, m := range models {
		sizeStr := formatSize(m.size)
		quantStr := m.quant
		if quantStr == "" {
			quantStr = "unknown"
		}
		fmt.Printf("  %-50s %s  %s\n",
			headerStyle.Render(m.repo),
			dimStyle.Render(quantStr),
			dimStyle.Render(sizeStr))
	}
	fmt.Println()

	if !localOnly {
		fmt.Printf("  %s\n\n", dimStyle.Render("Use 'hfetch search <query>' to find models on HuggingFace."))
	}

	return nil
}

func scanLocalModels(dataDir string) ([]localModel, error) {
	var models []localModel

	// hfetch stores models in dataDir/models/<org>/<repo>/
	modelsDir := filepath.Join(dataDir, "models")
	if _, err := os.Stat(modelsDir); os.IsNotExist(err) {
		return nil, nil
	}

	// Walk the models directory looking for .gguf files.
	err := filepath.Walk(modelsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".gguf") {
			return nil
		}

		// Derive repo name from path relative to modelsDir.
		rel, _ := filepath.Rel(modelsDir, path)
		parts := strings.SplitN(rel, string(os.PathSeparator), 3)
		repo := rel
		if len(parts) >= 2 {
			repo = parts[0] + "/" + parts[1]
		}

		quant := gguf.ParseQuantFromFilename(info.Name())

		models = append(models, localModel{
			repo:  repo,
			file:  info.Name(),
			quant: quant,
			size:  info.Size(),
		})
		return nil
	})

	return models, err
}

func formatSize(bytes int64) string {
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
