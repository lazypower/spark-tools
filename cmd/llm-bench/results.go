package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/config"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

func resultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "results",
		Short: "Manage benchmark results",
	}
	cmd.AddCommand(resultsListCmd(), resultsShowCmd())
	return cmd
}

func resultsListCmd() *cobra.Command {
	var (
		model  string
		after  string
		before string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored benchmark results",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			st := store.NewStore(dirs.Data + "/results")

			filter := store.StoreFilter{Model: model}
			if after != "" {
				t, err := time.Parse("2006-01-02", after)
				if err != nil {
					return fmt.Errorf("invalid --after date: %w", err)
				}
				filter.After = t
			}
			if before != "" {
				t, err := time.Parse("2006-01-02", before)
				if err != nil {
					return fmt.Errorf("invalid --before date: %w", err)
				}
				filter.Before = t
			}

			summaries, err := st.List(filter)
			if err != nil {
				return err
			}

			if len(summaries) == 0 {
				fmt.Println("No benchmark results found.")
				return nil
			}

			fmt.Printf("%-24s  %-30s  %5s  %s\n", "RUN ID", "SUITE", "JOBS", "DATE")
			fmt.Printf("%-24s  %-30s  %5s  %s\n", "------", "-----", "----", "----")
			for _, s := range summaries {
				date := s.StartedAt.Format("2006-01-02 15:04")
				name := s.SuiteName
				if len(name) > 30 {
					name = name[:27] + "..."
				}
				fmt.Printf("%-24s  %-30s  %5d  %s\n", s.RunID, name, s.JobCount, date)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Filter by model pattern")
	cmd.Flags().StringVar(&after, "after", "", "Results after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&before, "before", "", "Results before date (YYYY-MM-DD)")

	return cmd
}

func resultsShowCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "show <run_id>",
		Short: "Show results from a specific run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			st := store.NewStore(dirs.Data + "/results")

			result, err := st.Load(args[0])
			if err != nil {
				return err
			}

			switch format {
			case "json":
				data, err := report.JSONPretty(result)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			case "csv":
				data, err := report.CSV(result)
				if err != nil {
					return err
				}
				fmt.Print(string(data))
			default:
				fmt.Print(report.Terminal(result))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal, json, csv")

	return cmd
}
