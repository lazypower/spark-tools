package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/internal/tui"
	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
	"github.com/lazypower/spark-tools/pkg/llmrun/profiles"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func serveCmd() *cobra.Command {
	var (
		profileName string
		host        string
		port        int
		ctxSize     int
		parallel    int
		apiKey      string
		temp        float64
		systemText  string
		systemFile  string
		timeout     int
		noThink     bool
	)

	cmd := &cobra.Command{
		Use:   "serve [model]",
		Short: "Start OpenAI-compatible API server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelRef, err := resolveModelArg(args, profileName)
			if err != nil {
				return err
			}

			dirs := config.Dirs()
			gcfg := config.LoadGlobalConfig()

			var cfg engine.RunConfig
			if profileName != "" {
				store := profiles.NewProfileStore(dirs.Config)
				p, err := store.Get(profileName)
				if err != nil {
					return fmt.Errorf("loading profile %q: %w", profileName, err)
				}
				cfg = p.Config
			}
			if cfg.ReasoningBudget == 0 {
				cfg.ReasoningBudget = -1
			}

			cfg.ModelRef = modelRef
			cfg.ServerMode = true

			// Resolve model.
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

			// Hardware defaults.
			hw, _ := hardware.DetectHardware()
			if hw != nil {
				rec := hardware.RecommendConfig(hw, nil)
				applyDefaults(&cfg, rec)
			}

			// Flag overrides.
			if host != "" {
				cfg.Host = host
			} else if cfg.Host == "" {
				cfg.Host = "127.0.0.1"
			}
			if port > 0 {
				cfg.Port = port
			} else if cfg.Port == 0 {
				cfg.Port = 8080
			}
			if ctxSize > 0 {
				cfg.ContextSize = ctxSize
			}
			if parallel > 0 {
				cfg.Parallel = parallel
			}
			if apiKey != "" {
				cfg.APIKey = apiKey
			}
			if temp > 0 {
				cfg.Temperature = temp
			}
			if systemText != "" {
				cfg.SystemPrompt = systemText
			}
			if systemFile != "" {
				data, err := os.ReadFile(systemFile)
				if err != nil {
					return fmt.Errorf("reading system prompt file: %w", err)
				}
				cfg.SystemPrompt = string(data)
			}
			if noThink {
				cfg.ReasoningBudget = 0
			}

			// Detect llama.cpp.
			caps, err := engine.DetectBinaries(gcfg.LlamaDir)
			if err != nil {
				return fmt.Errorf("llama.cpp not found: %w", err)
			}

			// Print server header.
			printServerHeader(modelRef, resolved.Quant, cfg, hw)
			fmt.Println(tui.RenderServerEndpoints(cfg.Host, cfg.Port))
			fmt.Print("\n  Press Ctrl+C to stop.\n\n")

			// Launch server.
			proc, err := engine.Launch(context.Background(), cfg, *caps, dirs.Data)
			if err != nil {
				return err
			}

			endpoint := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)

			// Cancel readiness polling if the process dies.
			readyCtx, readyCancel := context.WithCancel(context.Background())
			go func() {
				select {
				case <-proc.Done():
					readyCancel()
				case <-readyCtx.Done():
				}
			}()

			startupTimeout := 120 * time.Second
			if timeout > 0 {
				startupTimeout = time.Duration(timeout) * time.Second
			}
			if err := engine.WaitForReady(readyCtx, endpoint, startupTimeout); err != nil {
				readyCancel()
				proc.Stop()
				if proc.Err() != nil {
					return fmt.Errorf("llama-server crashed during startup: %w\n\n%s", proc.Err(), formatCrashLog(proc))
				}
				return fmt.Errorf("server failed to start: %w", err)
			}
			readyCancel()

			fmt.Println("  Server ready.")

			// Wait for signal or unexpected process exit.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			select {
			case <-sigCh:
				fmt.Println("\n  Shutting down...")
				return proc.Stop()
			case <-proc.Done():
				return fmt.Errorf("llama-server exited unexpectedly: %w\n\n%s", proc.Err(), formatCrashLog(proc))
			}
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "Use a saved profile")
	cmd.Flags().StringVar(&host, "host", "", "Bind address (default: 127.0.0.1)")
	cmd.Flags().IntVar(&port, "port", 0, "Port (default: 8080)")
	cmd.Flags().IntVar(&ctxSize, "ctx", 0, "Context size (default: auto)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "Parallel request slots (default: 1)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Require API key for requests")
	cmd.Flags().Float64Var(&temp, "temp", 0, "Temperature (default: 0.7)")
	cmd.Flags().StringVar(&systemText, "system", "", "System prompt")
	cmd.Flags().StringVar(&systemFile, "system-file", "", "System prompt from file")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Server startup timeout in seconds (default: 120)")
	cmd.Flags().BoolVar(&noThink, "no-think", false, "Disable model thinking (--reasoning-budget 0)")

	return cmd
}
