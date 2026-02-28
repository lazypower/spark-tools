package report

import (
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

// CSV returns the run result as CSV data.
func CSV(result *store.RunResult) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Header
	header := []string{
		"job_id", "scenario", "run_index", "status",
		"model_ref", "quant",
		"context_size", "batch_size", "parallel_slots",
		"prompt_eval_tok_s_mean", "prompt_eval_tok_s_median",
		"generation_tok_s_mean", "generation_tok_s_median",
		"ttft_ms_mean", "ttft_ms_median", "ttft_ms_p95",
		"e2e_ms_mean", "e2e_ms_median",
		"model_load_ms",
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}

	// Rows
	for _, j := range result.Jobs {
		row := []string{
			j.JobID,
			j.ScenarioName,
			fmt.Sprintf("%d", j.RunIndex),
			string(j.Status),
			j.Model.NormalizedRef,
			j.Model.Quant,
			fmt.Sprintf("%d", j.EffectiveConfig.ContextSize),
			fmt.Sprintf("%d", j.EffectiveConfig.BatchSize),
			fmt.Sprintf("%d", j.EffectiveConfig.Parallel),
			ff(j.PromptEval.Mean),
			ff(j.PromptEval.Median),
			ff(j.Generation.Mean),
			ff(j.Generation.Median),
			ff(j.FirstTokenTime.Mean),
			ff(j.FirstTokenTime.Median),
			ff(j.FirstTokenTime.P95),
			ff(j.EndToEnd.Mean),
			ff(j.EndToEnd.Median),
			ff(j.ModelLoadTimeMs),
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}

	w.Flush()
	return buf.Bytes(), w.Error()
}

// CSVJobs returns just the job results as CSV (for filtering/comparing).
func CSVJobs(jobs []job.JobResult) ([]byte, error) {
	result := &store.RunResult{Jobs: jobs}
	return CSV(result)
}

func ff(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
