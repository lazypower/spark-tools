package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmrun"
)

func initCmd() *cobra.Command {
	var (
		models   string
		hardware bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a starter benchmark config",
		RunE: func(cmd *cobra.Command, args []string) error {
			config := generateStarterConfig(models, hardware)
			fmt.Print(config)
			return nil
		},
	}

	cmd.Flags().StringVar(&models, "models", "", "Pre-populate with model references (comma-separated)")
	cmd.Flags().BoolVar(&hardware, "hardware", false, "Auto-detect hardware and suggest scenarios")

	return cmd
}

func generateStarterConfig(models string, detectHW bool) string {
	var hwComment string
	if detectHW {
		hw, err := llmrun.DetectHardware()
		if err == nil && hw != nil {
			hwComment = fmt.Sprintf("# Detected: %s (%d cores), %.0f GB RAM",
				hw.CPUName, hw.CPUCores, hw.TotalMemoryGB)
			if len(hw.GPUs) > 0 {
				hwComment += fmt.Sprintf(", GPU: %s (%.0f GB)", hw.GPUs[0].Name, hw.GPUs[0].MemoryGB)
			}
			hwComment += "\n"
		}
	}

	modelSection := `models:
  - name: "Example-Model"
    ref: "owner/model-repo-GGUF"
    quants: ["Q4_K_M", "Q8_0"]
    alias: "example"
`

	if models != "" {
		modelSection = "models:\n"
		for _, m := range splitModels(models) {
			modelSection += fmt.Sprintf(`  - name: "%s"
    ref: "%s"
    quants: ["Q4_K_M"]
    alias: "%s"
`, m, m, m)
		}
	}

	return fmt.Sprintf(`%sname: "Benchmark Suite"
description: "Benchmark comparison"

defaults:
  warmup_prompts: 3
  measure_prompts: 10
  max_tokens: 512
  temperature: 0.0
  cooldown_seconds: 10
  timeout: 5m

%s
scenarios:
  - name: "throughput"
    description: "Raw generation throughput"
    context_sizes: [4096]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "short"
    max_tokens: 512
    repeat: 3

  - name: "context-scaling"
    description: "Performance across context sizes"
    context_sizes: [4096, 8192, 16384]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "medium"
    max_tokens: 256
    repeat: 2

settings:
  output_formats: ["json", "terminal"]
  system_check: true
  cooldown_between: 15
  server_startup_timeout: 2m
`, hwComment, modelSection)
}

func splitModels(s string) []string {
	var result []string
	for _, m := range splitCSV(s) {
		if m != "" {
			result = append(result, m)
		}
	}
	return result
}

func splitCSV(s string) []string {
	var parts []string
	for _, p := range []byte(s) {
		if p == ',' {
			parts = append(parts, "")
		} else if len(parts) == 0 {
			parts = []string{string(p)}
		} else {
			parts[len(parts)-1] += string(p)
		}
	}
	// Trim spaces
	for i := range parts {
		parts[i] = trimSpace(parts[i])
	}
	return parts
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

