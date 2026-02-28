package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/prompts"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/suite"
	"github.com/lazypower/spark-tools/pkg/llmrun"
)

func quickCmd() *cobra.Command {
	var (
		quant    string
		ctx      string
		nPrompts int
	)

	cmd := &cobra.Command{
		Use:   "quick <model>",
		Short: "Quick single-model benchmark",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelRef := args[0]

			eng, err := llmrun.NewEngine()
			if err != nil {
				return fmt.Errorf("initializing engine: %w", err)
			}

			// Parse context sizes
			ctxSizes := []int{4096}
			if ctx != "" {
				ctxSizes = nil
				for _, s := range strings.Split(ctx, ",") {
					var v int
					if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); err == nil {
						ctxSizes = append(ctxSizes, v)
					}
				}
			}

			if quant == "" {
				quant = "Q4_K_M"
			}

			// Load prompts
			promptList, err := prompts.LoadBuiltin("short")
			if err != nil {
				return err
			}

			// Build exec params
			params := job.ExecParams{
				JobID:          "quick-1",
				ScenarioName:   "quick",
				ModelRef:       fmt.Sprintf("%s:%s", modelRef, quant),
				ContextSize:    ctxSizes[0],
				BatchSize:      512,
				MaxTokens:      512,
				Temperature:    0.0,
				WarmupPrompts:  3,
				MeasurePrompts: nPrompts,
				ParallelSlots:  1,
			}

			// Signal handling
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			fmt.Printf("Quick Benchmark: %s (%s)\n", modelRef, quant)
			fmt.Printf("Context: %d | Prompts: %d\n", params.ContextSize, nPrompts)
			fmt.Println(strings.Repeat("=", 50))
			fmt.Println()

			executor := job.NewExecutor(eng, 500, 2*time.Minute)
			result := executor.Execute(runCtx, params, promptList)

			// Use suite.Duration for wrapping
			result.Duration = job.Duration{Duration: time.Duration(result.Duration.Duration)}

			fmt.Println()
			fmt.Print(report.QuickResult(result))

			return nil
		},
	}

	cmd.Flags().StringVar(&quant, "quant", "", "Quantization to test (default: Q4_K_M)")
	cmd.Flags().StringVar(&ctx, "ctx", "", "Context sizes (comma-separated)")
	cmd.Flags().IntVar(&nPrompts, "prompts", 10, "Number of measurement prompts")

	return cmd
}

// synthetic creates a minimal BenchmarkSuite for quick benchmarks.
func synthetic(modelRef, quant string, ctxSizes []int, nPrompts int) *suite.BenchmarkSuite {
	return &suite.BenchmarkSuite{
		Name: "Quick Benchmark",
		Defaults: suite.JobDefaults{
			WarmupPrompts:  3,
			MeasurePrompts: nPrompts,
			MaxTokens:      512,
			CooldownSeconds: 0,
			Timeout:         suite.Duration{Duration: 5 * time.Minute},
		},
		Models: []suite.ModelSpec{
			{
				Name:   modelRef,
				Ref:    modelRef,
				Quants: []string{quant},
				Alias:  "quick",
			},
		},
		Scenarios: []suite.Scenario{
			{
				Name:          "quick",
				ContextSizes:  ctxSizes,
				BatchSizes:    []int{512},
				ParallelSlots: []int{1},
				Prompts:       suite.PromptSet{Builtin: "short"},
				MaxTokens:     512,
				Repeat:        1,
			},
		},
	}
}
