package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/tui"
	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/api"
	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
	"github.com/lazypower/spark-tools/pkg/llmrun/profiles"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func chatCmd() *cobra.Command {
	var (
		profileName string
		ctxSize     int
		temp        float64
		systemText  string
		systemFile  string
	)

	cmd := &cobra.Command{
		Use:   "chat [model]",
		Short: "Interactive chat session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelRef, err := resolveModelArg(args, profileName)
			if err != nil {
				return err
			}
			return runInference(modelRef, inferenceFlags{
				profileName: profileName,
				ctxSize:     ctxSize,
				temp:        temp,
				systemText:  systemText,
				systemFile:  systemFile,
				interactive: true,
			})
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "Use a saved profile")
	cmd.Flags().IntVar(&ctxSize, "ctx", 0, "Context size (default: auto)")
	cmd.Flags().Float64Var(&temp, "temp", 0, "Temperature (default: 0.7)")
	cmd.Flags().StringVar(&systemText, "system", "", "System prompt")
	cmd.Flags().StringVar(&systemFile, "system-file", "", "System prompt from file")

	return cmd
}

// resolveModelArg extracts the model ref from args, falling back to
// the profile's model or LLM_RUN_DEFAULT_MODEL if no arg was given.
func resolveModelArg(args []string, profileName string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	// Try profile's model ref.
	if profileName != "" {
		dirs := config.Dirs()
		store := profiles.NewProfileStore(dirs.Config)
		p, err := store.Get(profileName)
		if err == nil && p.Config.ModelRef != "" {
			return p.Config.ModelRef, nil
		}
	}

	// Try global default.
	gcfg := config.LoadGlobalConfig()
	if gcfg.DefaultModel != "" {
		return gcfg.DefaultModel, nil
	}

	return "", fmt.Errorf("no model specified. Provide a model argument, use --profile, or set LLM_RUN_DEFAULT_MODEL")
}

type inferenceFlags struct {
	profileName string
	ctxSize     int
	temp        float64
	systemText  string
	systemFile  string
	interactive bool
	host        string
	port        int
	parallel    int
	apiKey      string
	prompt      string
	promptFile  string
	format      string
	maxTokens   int
}

func runInference(modelRef string, flags inferenceFlags) error {
	dirs := config.Dirs()
	gcfg := config.LoadGlobalConfig()

	// Resolve profile: explicit flag → global default → none.
	profileName := flags.profileName
	if profileName == "" && gcfg.DefaultProfile != "" {
		profileName = gcfg.DefaultProfile
	}
	var cfg engine.RunConfig
	if profileName != "" {
		store := profiles.NewProfileStore(dirs.Config)
		p, err := store.Get(profileName)
		if err != nil {
			// If using the global default and it doesn't exist, just skip it.
			if flags.profileName != "" {
				return fmt.Errorf("loading profile %q: %w", profileName, err)
			}
		} else {
			cfg = p.Config
		}
	}

	// Model ref overrides profile's model.
	cfg.ModelRef = modelRef

	// Resolve model to a local path.
	hfDirs := hfconfig.Dirs()
	res := resolver.NewResolver(dirs.Config, hfDirs.Data)
	resolved, err := res.ResolveModel(context.Background(), modelRef)
	if err != nil {
		return err
	}
	if resolved.Path == "" {
		return fmt.Errorf("model not found locally: %s\n  Use 'hfetch pull %s' to download it first", modelRef, modelRef)
	}
	cfg.ModelPath = resolved.Path

	// Detect hardware and recommend defaults for unset fields.
	hw, _ := hardware.DetectHardware()
	if hw != nil {
		rec := hardware.RecommendConfig(hw, nil)
		applyDefaults(&cfg, rec)
	}

	// Apply flag overrides.
	if flags.ctxSize > 0 {
		cfg.ContextSize = flags.ctxSize
	}
	if flags.temp > 0 {
		cfg.Temperature = flags.temp
	}
	if flags.systemText != "" {
		cfg.SystemPrompt = flags.systemText
	}
	if flags.systemFile != "" {
		data, err := os.ReadFile(flags.systemFile)
		if err != nil {
			return fmt.Errorf("reading system prompt file: %w", err)
		}
		cfg.SystemPrompt = string(data)
	}
	if flags.host != "" {
		cfg.Host = flags.host
	}
	if flags.port > 0 {
		cfg.Port = flags.port
	}
	if flags.parallel > 0 {
		cfg.Parallel = flags.parallel
	}
	if flags.apiKey != "" {
		cfg.APIKey = flags.apiKey
	}

	// Detect llama.cpp.
	llamaDir := gcfg.LlamaDir
	caps, err := engine.DetectBinaries(llamaDir)
	if err != nil {
		return fmt.Errorf("llama.cpp not found: %w\n\n  Set LLM_RUN_LLAMA_DIR to your llama.cpp build directory,\n  or ensure llama-server is on your PATH.", err)
	}

	if flags.interactive {
		// Chat mode: launch server, wait for ready, then TUI.
		cfg.ServerMode = true
		if cfg.Host == "" {
			cfg.Host = "127.0.0.1"
		}
		if cfg.Port == 0 {
			cfg.Port = 8080
		}

		proc, err := engine.Launch(context.Background(), cfg, *caps, dirs.Data)
		if err != nil {
			return err
		}
		defer proc.Stop()

		endpoint := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)

		readyCtx, readyCancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-proc.Done():
				readyCancel()
			case <-readyCtx.Done():
			}
		}()

		if err := engine.WaitForReady(readyCtx, endpoint, 60*time.Second); err != nil {
			readyCancel()
			if proc.Err() != nil {
				return fmt.Errorf("llama-server crashed during startup: %w\n\n%s", proc.Err(), formatCrashLog(proc))
			}
			return fmt.Errorf("server failed to start: %w", err)
		}
		readyCancel()

		client := api.NewClient(endpoint)
		return tui.RunChat(tui.ChatConfig{
			Client:      client,
			ModelName:   modelRef,
			Quant:       resolved.Quant,
			ContextSize: cfg.ContextSize,
			GPUName:     gpuName(hw),
			Threads:     cfg.Threads,
		})
	}

	// Non-interactive: single prompt mode.
	cfg.ServerMode = true
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	proc, err := engine.Launch(context.Background(), cfg, *caps, dirs.Data)
	if err != nil {
		return err
	}
	defer proc.Stop()

	endpoint := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)

	readyCtx, readyCancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-proc.Done():
			readyCancel()
		case <-readyCtx.Done():
		}
	}()

	if err := engine.WaitForReady(readyCtx, endpoint, 60*time.Second); err != nil {
		readyCancel()
		if proc.Err() != nil {
			return fmt.Errorf("llama-server crashed during startup: %w\n\n%s", proc.Err(), formatCrashLog(proc))
		}
		return fmt.Errorf("server failed to start: %w", err)
	}
	readyCancel()

	// Read prompt.
	prompt := flags.prompt
	if flags.promptFile != "" {
		data, err := os.ReadFile(flags.promptFile)
		if err != nil {
			return fmt.Errorf("reading prompt file: %w", err)
		}
		prompt = string(data)
	}
	if prompt == "" {
		return fmt.Errorf("--prompt or --prompt-file required for non-interactive mode")
	}

	client := api.NewClient(endpoint)
	req := api.ChatCompletionRequest{
		Messages:  []api.Message{{Role: "user", Content: prompt}},
		MaxTokens: flags.maxTokens,
		Stop:      []string{"<|im_start|>", "<|im_end|>"},
	}
	if flags.format != "" {
		req.ResponseFormat = &api.ResponseFormat{Type: flags.format}
	}
	resp, err := client.ChatCompletion(context.Background(), req)
	if err != nil {
		return err
	}
	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
		fmt.Println(resp.Choices[0].Message.Content)
	}
	return nil
}

func applyDefaults(cfg *engine.RunConfig, rec engine.RunConfig) {
	if cfg.Threads == 0 {
		cfg.Threads = rec.Threads
	}
	if cfg.GPULayers == 0 {
		cfg.GPULayers = rec.GPULayers
	}
	if cfg.ContextSize == 0 {
		cfg.ContextSize = rec.ContextSize
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = rec.BatchSize
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = rec.Temperature
	}
	if !cfg.FlashAttention && rec.FlashAttention {
		cfg.FlashAttention = true
	}
	if !cfg.MMap && rec.MMap {
		cfg.MMap = true
	}
	if !cfg.MLock && rec.MLock {
		cfg.MLock = true
	}
	if cfg.NumaStrategy == engine.NumaDisabled && rec.NumaStrategy != engine.NumaDisabled {
		cfg.NumaStrategy = rec.NumaStrategy
	}
}

func gpuName(hw *hardware.HardwareInfo) string {
	if hw == nil || len(hw.GPUs) == 0 {
		return ""
	}
	return hw.GPUs[0].Name
}

// formatCrashLog reads the tail of the server log and formats it for display.
func formatCrashLog(proc *engine.Process) string {
	log := proc.CrashLog(4096)
	if log == "" {
		return fmt.Sprintf("  Log file: %s", proc.LogFile)
	}
	return fmt.Sprintf("  --- server log (last 4KB) ---\n%s\n  --- end log ---\n  Full log: %s", log, proc.LogFile)
}

func printServerHeader(modelRef, quant string, cfg engine.RunConfig, hw *hardware.HardwareInfo) {
	headerStyle := lipgloss.NewStyle().Bold(true)
	fmt.Printf("\n  %s\n", headerStyle.Render("llm-run server"))
	fmt.Printf("  Model:   %s", modelRef)
	if quant != "" {
		fmt.Printf(" (%s)", quant)
	}
	fmt.Println()
	fmt.Printf("  API:     http://%s:%d/v1\n", cfg.Host, cfg.Port)
	fmt.Printf("  Context: %d tokens", cfg.ContextSize)
	if cfg.Parallel > 0 {
		fmt.Printf(" │ Parallel slots: %d", cfg.Parallel)
	}
	fmt.Println()
	if hw != nil && len(hw.GPUs) > 0 {
		layers := "all layers offloaded"
		if cfg.GPULayers >= 0 {
			layers = fmt.Sprintf("%d layers offloaded", cfg.GPULayers)
		}
		fmt.Printf("  GPU:     %s (%s)\n", hw.GPUs[0].Name, layers)
	}
	fmt.Println()
}
