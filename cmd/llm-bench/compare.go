package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/config"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

func compareCmd() *cobra.Command {
	var (
		metric string
		format string
	)

	cmd := &cobra.Command{
		Use:   "compare <run_id...>",
		Short: "Compare results across runs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			st := store.NewStore(dirs.Data + "/results")

			var results []*store.RunResult
			for _, runID := range args {
				result, err := st.Load(runID)
				if err != nil {
					return fmt.Errorf("loading %s: %w", runID, err)
				}
				results = append(results, result)
			}

			switch format {
			case "json":
				// Merge all jobs into one result for JSON output
				merged := results[0]
				for _, r := range results[1:] {
					merged.Jobs = append(merged.Jobs, r.Jobs...)
				}
				data, err := report.JSONPretty(merged)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			case "csv":
				merged := results[0]
				for _, r := range results[1:] {
					merged.Jobs = append(merged.Jobs, r.Jobs...)
				}
				data, err := report.CSV(merged)
				if err != nil {
					return err
				}
				fmt.Print(string(data))
			default:
				fmt.Print(report.Compare(results, metric))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&metric, "metric", "", "Focus on specific metric (generation, prompt, ttft, e2e)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal, json, csv")

	return cmd
}
