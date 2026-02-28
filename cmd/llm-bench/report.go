package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmbench/config"
	"github.com/lazypower/spark-tools/pkg/llmbench/report"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

func reportCmd() *cobra.Command {
	var (
		format string
		output string
	)

	cmd := &cobra.Command{
		Use:   "report <run_id>",
		Short: "Generate a formatted report",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			st := store.NewStore(dirs.Data + "/results")

			result, err := st.Load(args[0])
			if err != nil {
				return err
			}

			var content []byte
			switch format {
			case "json":
				content, err = report.JSONPretty(result)
			case "csv":
				content, err = report.CSV(result)
			default:
				content = []byte(report.Terminal(result))
			}
			if err != nil {
				return err
			}

			if output != "" {
				return os.WriteFile(output, content, 0644)
			}
			fmt.Print(string(content))
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal, json, csv")
	cmd.Flags().StringVar(&output, "output", "", "Write to file instead of stdout")

	return cmd
}
