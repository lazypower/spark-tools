package report

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

// Compare renders a comparison table across multiple run results.
// It produces a model x quant matrix for each metric.
func Compare(results []*store.RunResult, metric string) string {
	// Collect all successful jobs across all runs
	var allJobs []job.JobResult
	for _, r := range results {
		for _, j := range r.Jobs {
			if j.Status == job.JobStatusOK {
				allJobs = append(allJobs, j)
			}
		}
	}

	if len(allJobs) == 0 {
		return dimStyle.Render("No successful jobs to compare.")
	}

	// Build unique models and quants
	modelSet := make(map[string]bool)
	quantSet := make(map[string]bool)
	var models, quants []string

	for _, j := range allJobs {
		ref := j.Model.NormalizedRef
		if !modelSet[ref] {
			modelSet[ref] = true
			models = append(models, ref)
		}
		q := j.Model.Quant
		if !quantSet[q] {
			quantSet[q] = true
			quants = append(quants, q)
		}
	}

	// Build lookup
	type key struct {
		model, quant string
	}
	lookup := make(map[key]metrics.ThroughputStats)
	for _, j := range allJobs {
		k := key{j.Model.NormalizedRef, j.Model.Quant}
		stat := selectMetric(j, metric)
		// If multiple runs, keep the one with more samples
		if existing, ok := lookup[k]; !ok || stat.Samples > existing.Samples {
			lookup[k] = stat
		}
	}

	// Render
	var b strings.Builder

	title := metricTitle(metric)
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n")

	// Column widths
	modelWidth := 24
	for _, m := range models {
		if len(m) > modelWidth {
			modelWidth = len(m) + 2
		}
	}
	colWidth := 10

	// Header row
	b.WriteString(padRight("Model", modelWidth))
	for _, q := range quants {
		b.WriteString(padRight(q, colWidth))
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", modelWidth+colWidth*len(quants)))
	b.WriteString("\n")

	// Data rows
	for _, m := range models {
		shortName := m
		if len(shortName) > modelWidth-2 {
			shortName = shortName[:modelWidth-5] + "..."
		}
		b.WriteString(padRight(shortName, modelWidth))
		for _, q := range quants {
			k := key{m, q}
			if stat, ok := lookup[k]; ok {
				b.WriteString(padRight(formatValue(stat, metric), colWidth))
			} else {
				b.WriteString(padRight(lipgloss.NewStyle().Faint(true).Render("—"), colWidth))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func selectMetric(j job.JobResult, metric string) metrics.ThroughputStats {
	switch metric {
	case "generation", "gen", "":
		return j.Generation
	case "prompt_eval", "prompt":
		return j.PromptEval
	case "ttft":
		return j.FirstTokenTime
	case "e2e":
		return j.EndToEnd
	default:
		return j.Generation
	}
}

func metricTitle(metric string) string {
	switch metric {
	case "generation", "gen", "":
		return "Generation Speed (tok/s)"
	case "prompt_eval", "prompt":
		return "Prompt Eval Speed (tok/s)"
	case "ttft":
		return "Time to First Token (ms, median)"
	case "e2e":
		return "End-to-End Latency (ms, median)"
	default:
		return "Generation Speed (tok/s)"
	}
}

func formatValue(s metrics.ThroughputStats, metric string) string {
	if s.Samples == 0 {
		return "—"
	}
	switch metric {
	case "ttft", "e2e":
		return fmt.Sprintf("%.0f", s.Median)
	default:
		return fmt.Sprintf("%.1f", s.Median)
	}
}
