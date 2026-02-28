package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
	"github.com/lazypower/spark-tools/pkg/llmrun/profiles"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func explainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <topic>",
		Short: "Explain a parameter or show effective config",
		Long: `Explain a llm-run parameter or concept.

Available topics:
  context-size     Context window size
  batch-size       Prompt processing batch size
  gpu-layers       GPU layer offloading
  temperature      Sampling temperature
  flash-attention  Flash attention optimization
  mmap             Memory-mapped model loading
  mlock            Lock model in memory
  numa             NUMA memory allocation
  top-p            Nucleus sampling
  top-k            Top-K sampling

Use 'llm-run explain effective <model>' to show the full computed config.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "effective" {
				profileName, _ := cmd.Flags().GetString("profile")
				if len(args) < 2 {
					return fmt.Errorf("usage: llm-run explain effective <model> [--profile <name>]")
				}
				return showEffectiveConfig(args[1], profileName)
			}
			return explainTopic(args[0])
		},
	}

	cmd.Flags().String("profile", "", "Include profile overrides (for 'effective')")

	return cmd
}

var explanations = map[string]string{
	"context-size": `
  Context Size (--ctx)
  ────────────────────
  The maximum number of tokens the model can "see" at once, including
  both the conversation history and its response.

  Higher context = more memory for KV cache, slower first-token time.
  For coding tasks, 32K is usually sufficient. For long documents,
  you may need 64K+.

  Default: auto-detected based on your hardware and model.
  Override: llm-run chat <model> --ctx 32768`,

	"batch-size": `
  Batch Size (--batch-size)
  ────────────────────────
  The number of tokens processed simultaneously during prompt ingestion.
  Larger batch sizes improve prompt processing speed at the cost of
  more memory.

  Default: auto-scaled based on available memory (256-2048).`,

	"gpu-layers": `
  GPU Layers (--n-gpu-layers)
  ──────────────────────────
  Number of model layers to offload to GPU memory. Set to -1 to offload
  all layers. Set to 0 for CPU-only inference.

  On unified memory architectures (DGX Spark), all layers are offloaded
  by default since CPU and GPU share the same memory pool.

  Default: -1 (all) if GPU detected, 0 if CPU-only.`,

	"temperature": `
  Temperature (--temp)
  ───────────────────
  Controls randomness in token sampling. Lower = more deterministic,
  higher = more creative/varied.

  Recommended ranges:
    0.1      Factual, analytical tasks
    0.3      Code generation
    0.7      General conversation (default)
    0.9+     Creative writing, brainstorming

  Default: 0.7`,

	"flash-attention": `
  Flash Attention (--flash-attn)
  ─────────────────────────────
  An optimized attention implementation that reduces memory usage and
  increases speed, especially with long contexts. Requires llama.cpp
  built with flash attention support.

  If requested but not supported, llm-run will warn and continue without it.

  Default: enabled if supported.`,

	"mmap": `
  Memory-Mapped Loading (--mmap)
  ─────────────────────────────
  Maps the model file directly into virtual memory instead of reading
  it into a buffer. Reduces startup time and allows the OS to manage
  paging. Recommended for most setups.

  Default: enabled.`,

	"mlock": `
  Memory Lock (--mlock)
  ────────────────────
  Locks the model in physical memory, preventing the OS from swapping
  it to disk. Useful on dedicated inference machines where you want
  guaranteed performance. Increases memory pressure.

  Default: disabled (enabled on DGX Spark).`,

	"numa": `
  NUMA Strategy (--numa)
  ─────────────────────
  Controls how memory is allocated across NUMA nodes on multi-socket
  or multi-node systems.

  Strategies:
    disabled     No NUMA awareness (default)
    distribute   Spread memory across all NUMA nodes
    isolate      Restrict to a single NUMA node

  Default: disabled (distribute on DGX Spark).`,

	"top-p": `
  Top-P / Nucleus Sampling (--top-p)
  ──────────────────────────────────
  Only consider tokens whose cumulative probability exceeds this threshold.
  Lower values = more focused; higher values = more diverse.

  Default: 0.9 for most profiles.`,

	"top-k": `
  Top-K Sampling (--top-k)
  ────────────────────────
  Only consider the top K most likely tokens at each step.
  Set to 0 to disable. Lower values = more focused output.

  Default: 40`,
}

func explainTopic(topic string) error {
	text, ok := explanations[topic]
	if !ok {
		fmt.Printf("Unknown topic: %q\n\nAvailable topics:\n", topic)
		for k := range explanations {
			fmt.Printf("  %s\n", k)
		}
		return nil
	}
	fmt.Println(text)
	return nil
}

func showEffectiveConfig(modelRef, profileName string) error {
	dirs := config.Dirs()
	gcfg := config.LoadGlobalConfig()

	headerStyle := lipgloss.NewStyle().Bold(true)

	var cfg engine.RunConfig

	// Load profile if specified.
	if profileName != "" {
		store := profiles.NewProfileStore(dirs.Config)
		p, err := store.Get(profileName)
		if err != nil {
			return fmt.Errorf("loading profile %q: %w", profileName, err)
		}
		cfg = p.Config
		fmt.Printf("\n  %s %s\n", headerStyle.Render("Profile:"), profileName)
	}

	cfg.ModelRef = modelRef

	// Resolve model.
	hfDirs := hfconfig.Dirs()
	res := resolver.NewResolver(dirs.Config, hfDirs.Data)
	resolved, err := res.ResolveModel(context.Background(), modelRef)
	if err != nil {
		return fmt.Errorf("resolving model: %w", err)
	}

	fmt.Printf("\n  %s\n", headerStyle.Render("Model Resolution"))
	fmt.Printf("    Requested:  %s\n", modelRef)
	fmt.Printf("    Source:     %s\n", resolved.Source)
	if resolved.Path != "" {
		fmt.Printf("    Path:       %s\n", resolved.Path)
		cfg.ModelPath = resolved.Path
	}
	if resolved.Quant != "" {
		fmt.Printf("    Quant:      %s\n", resolved.Quant)
	}

	// Hardware detection.
	hw, _ := hardware.DetectHardware()
	if hw != nil {
		rec := hardware.RecommendConfig(hw, nil)
		applyDefaults(&cfg, rec)
	}

	// Detect llama.cpp.
	caps, err := engine.DetectBinaries(gcfg.LlamaDir)
	if err != nil {
		fmt.Printf("\n  %s %v\n", headerStyle.Render("llama.cpp:"), err)
	} else {
		fmt.Printf("\n  %s\n", headerStyle.Render("llama.cpp"))
		fmt.Printf("    Binary:     %s\n", caps.BinaryPath)
		fmt.Printf("    Version:    %s\n", caps.Version)
		fmt.Printf("    Backend:    %s\n", caps.Backend)

		// Build the effective command line.
		cfg.ServerMode = true
		if cfg.Host == "" {
			cfg.Host = "127.0.0.1"
		}
		if cfg.Port == 0 {
			cfg.Port = 8080
		}

		cmdLine, warnings, buildErr := engine.BuildCommand(cfg, *caps)
		if buildErr != nil {
			fmt.Printf("\n  %s %v\n", headerStyle.Render("Build Error:"), buildErr)
		} else {
			fmt.Printf("\n  %s\n", headerStyle.Render("Effective Command"))
			for i, arg := range cmdLine {
				if i == 0 {
					fmt.Printf("    %s", arg)
				} else {
					fmt.Printf(" \\\n      %s", arg)
				}
			}
			fmt.Println()

			for _, w := range warnings {
				fmt.Printf("    ⚠ %s\n", w)
			}
		}
	}

	// Print effective config as JSON.
	fmt.Printf("\n  %s\n", headerStyle.Render("Effective RunConfig"))
	data, _ := json.MarshalIndent(cfg, "    ", "  ")
	fmt.Printf("    %s\n\n", string(data))

	return nil
}
