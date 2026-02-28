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

			if err := engine.WaitForReady(readyCtx, endpoint, 120*time.Second); err != nil {
				readyCancel()
				proc.Stop()
				if proc.Err() != nil {
					return fmt.Errorf("llama-server crashed during startup: %w", proc.Err())
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
				fmt.Println("\n  Server process exited unexpectedly.")
				if err := proc.Err(); err != nil {
					return fmt.Errorf("llama-server crashed: %w", err)
				}
				return fmt.Errorf("llama-server exited unexpectedly")
			}
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "Use a saved profile")
	cmd.Flags().StringVar(&host, "host", "", "Bind address (default: 127.0.0.1)")
	cmd.Flags().IntVar(&port, "port", 0, "Port (default: 8080)")
	cmd.Flags().IntVar(&ctxSize, "ctx", 0, "Context size (default: auto)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "Parallel request slots (default: 1)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Require API key for requests")

	return cmd
}
