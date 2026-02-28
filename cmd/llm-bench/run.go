package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/config"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
	"github.com/lazypower/spark-tools/pkg/llmbench/suite"
	"github.com/lazypower/spark-tools/pkg/llmbench/syscheck"
	"github.com/lazypower/spark-tools/pkg/llmrun"
)

func runCmd() *cobra.Command {
	var (
		dryRun       bool
		yes          bool
		jobs         string
		outputDir    string
		skipCheck    bool
		dirtyMode    string
		continueFrom string
	)

	cmd := &cobra.Command{
		Use:   "run <config.yaml>",
		Short: "Run a benchmark suite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := args[0]

			s, err := suite.LoadSuite(configPath)
			if err != nil {
				return err
			}

			// Build runner options
			var opts []suite.RunnerOption

			if jobs != "" {
				patterns := strings.Split(jobs, ",")
				opts = append(opts, suite.WithJobFilter(patterns))
			}

			// Dry run
			if dryRun {
				runner := suite.NewRunner(opts...)
				expanded := runner.DryRun(s)
				fmt.Printf("Dry run: %d jobs would be executed\n\n", len(expanded))
				for i, j := range expanded {
					fmt.Printf("  %3d. %s\n", i+1, j.JobID)
				}
				return nil
			}

			// Engine
			eng, err := llmrun.NewEngine()
			if err != nil {
				return fmt.Errorf("initializing engine: %w", err)
			}
			opts = append(opts, suite.WithEngine(eng))

			// Store
			dirs := config.Dirs()
			storeDir := dirs.Data + "/results"
			if outputDir != "" {
				storeDir = outputDir
			}
			st := store.NewStore(storeDir)
			opts = append(opts, suite.WithStore(st))

			if outputDir != "" {
				opts = append(opts, suite.WithOutputDir(outputDir))
			}
			if skipCheck {
				opts = append(opts, suite.WithSkipCheck(true))
			}
			if dirtyMode != "" {
				dm, err := syscheck.ParseDirtyMode(dirtyMode)
				if err != nil {
					return err
				}
				opts = append(opts, suite.WithDirtyMode(dm))
			}
			if continueFrom != "" {
				opts = append(opts, suite.WithContinueFrom(continueFrom))
			}

			// Progress
			opts = append(opts, suite.WithProgressFunc(func(current, total int, jobID, status string) {
				fmt.Printf("  [%d/%d] %s — %s\n", current, total, jobID, status)
			}))

			runner := suite.NewRunner(opts...)

			// Confirmation
			if !yes {
				expanded := runner.DryRun(s)
				fmt.Printf("Ready to run %d benchmark jobs\n", len(expanded))
				if !confirmRun() {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// Run with signal handling
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\nInterrupted — saving partial results...")
				cancel()
			}()

			result, err := runner.Run(ctx, s)
			if err != nil && ctx.Err() == nil {
				return err
			}

			if result != nil {
				// Save config copy
				configData, _ := os.ReadFile(configPath)
				st.SaveConfig(result.RunID, configData)

				fmt.Println()
				fmt.Print(report.Terminal(result))
				fmt.Printf("\nResults saved to: %s/%s\n", storeDir, result.RunID)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show job matrix without executing")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&jobs, "jobs", "", "Filter jobs by pattern (comma-separated)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Override output directory")
	cmd.Flags().BoolVar(&skipCheck, "skip-check", false, "Skip pre-flight system checks")
	cmd.Flags().StringVar(&dirtyMode, "dirty-mode", "", "Override dirty_mode (abort/warn/force)")
	cmd.Flags().StringVar(&continueFrom, "continue-from", "", "Resume from a specific job ID")

	return cmd
}
