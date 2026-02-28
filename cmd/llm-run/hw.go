package main

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
)

func hwCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "hw",
		Short: "Show detected hardware and recommendations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			hw, err := hardware.DetectHardware()
			if err != nil {
				return fmt.Errorf("detecting hardware: %w", err)
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(hw, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			printHardwareInfo(hw)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")

	return cmd
}

func printHardwareInfo(hw *hardware.HardwareInfo) {
	headerStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Faint(true)

	fmt.Printf("\n  %s\n\n", headerStyle.Render("Hardware Detection"))

	// CPU
	fmt.Printf("  %s\n", headerStyle.Render("CPU"))
	fmt.Printf("    Name:   %s\n", hw.CPUName)
	fmt.Printf("    Cores:  %d\n", hw.CPUCores)
	fmt.Println()

	// Memory
	fmt.Printf("  %s\n", headerStyle.Render("Memory"))
	fmt.Printf("    Total:     %.1f GB\n", hw.TotalMemoryGB)
	fmt.Printf("    Available: %.1f GB\n", hw.FreeMemoryGB)
	fmt.Println()

	// GPU
	fmt.Printf("  %s\n", headerStyle.Render("GPU"))
	if len(hw.GPUs) == 0 {
		fmt.Printf("    %s\n", dimStyle.Render("No GPU detected"))
	} else {
		for _, gpu := range hw.GPUs {
			fmt.Printf("    [%d] %s", gpu.Index, gpu.Name)
			if gpu.MemoryGB > 0 {
				fmt.Printf(" (%.1f GB)", gpu.MemoryGB)
			}
			if gpu.Compute != "" {
				fmt.Printf(" [%s]", gpu.Compute)
			}
			fmt.Println()
		}
	}
	fmt.Println()

	// NUMA
	if hw.NUMANodes > 0 {
		fmt.Printf("  %s\n", headerStyle.Render("NUMA"))
		fmt.Printf("    Nodes: %d\n", hw.NUMANodes)
		fmt.Println()
	}

	// DGX Spark detection
	if hw.IsDGXSpark {
		fmt.Printf("  %s %s\n\n",
			headerStyle.Render("Platform:"),
			"NVIDIA DGX Spark GB10 (detected)")
	}

	// Recommendations
	rec := hardware.RecommendConfig(hw, nil)
	fmt.Printf("  %s\n", headerStyle.Render("Recommended Defaults"))
	fmt.Printf("    Threads:        %d\n", rec.Threads)
	fmt.Printf("    GPU Layers:     %d", rec.GPULayers)
	if rec.GPULayers == -1 {
		fmt.Printf(" (all)")
	}
	fmt.Println()
	fmt.Printf("    Context Size:   %d tokens\n", rec.ContextSize)
	fmt.Printf("    Batch Size:     %d\n", rec.BatchSize)
	fmt.Printf("    Flash Attn:     %v\n", rec.FlashAttention)
	fmt.Printf("    MMap:           %v\n", rec.MMap)
	fmt.Printf("    MLock:          %v\n", rec.MLock)
	if rec.NumaStrategy != 0 {
		fmt.Printf("    NUMA Strategy:  %s\n", rec.NumaStrategy.String())
	}
	fmt.Println()
}
