// Package report generates benchmark output in terminal, JSON,
// CSV, and markdown formats, plus cross-run comparisons.
package report

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
)

// Terminal renders a run result as a styled terminal table.
func Terminal(result *store.RunResult) string {
	var b strings.Builder

	// Header
	b.WriteString(headerStyle.Render(result.SuiteName))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("Run: %s  |  %d jobs  |  %s",
		result.RunID,
		len(result.Jobs),
		result.CompletedAt.Sub(result.StartedAt).Round(1e9).String(),
	)))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 60))
	b.WriteString("\n\n")

	// Jobs
	for _, j := range result.Jobs {
		b.WriteString(formatJobResult(j))
		b.WriteString("\n")
	}

	// Summary
	ok, failed, skipped := countStatuses(result.Jobs)
	b.WriteString(dimStyle.Render(fmt.Sprintf("Summary: %d OK, %d failed, %d skipped", ok, failed, skipped)))
	b.WriteString("\n")

	return b.String()
}

// QuickResult renders a compact result for quick benchmarks.
func QuickResult(j job.JobResult) string {
	var b strings.Builder

	if j.Status != job.JobStatusOK {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Failed: %s", j.Error.Message)))
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString(headerStyle.Render(fmt.Sprintf("Quick Benchmark: %s", j.Model.NormalizedRef)))
	b.WriteString("\n\n")

	rows := []struct {
		label string
		value string
	}{
		{"Prompt Processing", formatThroughput(j.PromptEval, "tok/s")},
		{"Generation", formatThroughput(j.Generation, "tok/s")},
		{"Time to First Token", formatThroughput(j.FirstTokenTime, "ms")},
		{"Model Load Time", fmt.Sprintf("%.1fs", j.ModelLoadTimeMs/1000)},
	}

	if j.SystemMetrics != nil && j.SystemMetrics.Available {
		rows = append(rows, struct{ label, value string }{
			"Peak Memory", fmt.Sprintf("%.1f GB", float64(j.SystemMetrics.PeakMemoryMB)/1024),
		})
	}

	maxLabel := 0
	for _, r := range rows {
		if len(r.label) > maxLabel {
			maxLabel = len(r.label)
		}
	}

	var lines []string
	for _, r := range rows {
		line := fmt.Sprintf("  %s  %s",
			labelStyle.Render(padRight(r.label, maxLabel)),
			r.value)
		lines = append(lines, line)
	}

	b.WriteString(boxStyle.Render(strings.Join(lines, "\n")))
	b.WriteString("\n")

	return b.String()
}

func formatJobResult(j job.JobResult) string {
	var b strings.Builder

	status := labelStyle.Render("OK")
	if j.Status == job.JobStatusFailed {
		status = errorStyle.Render("FAIL")
	} else if j.Status == job.JobStatusSkipped {
		status = dimStyle.Render("SKIP")
	}

	b.WriteString(fmt.Sprintf("  [%s] %s\n", status, j.JobID))

	if j.Status == job.JobStatusOK {
		b.WriteString(fmt.Sprintf("    Prompt Eval: %s\n", formatThroughput(j.PromptEval, "tok/s")))
		b.WriteString(fmt.Sprintf("    Generation:  %s\n", formatThroughput(j.Generation, "tok/s")))
		b.WriteString(fmt.Sprintf("    TTFT:        %s\n", formatThroughput(j.FirstTokenTime, "ms")))
	} else if j.Status == job.JobStatusFailed && j.Error != nil {
		b.WriteString(fmt.Sprintf("    Error: %s (%s)\n", j.Error.Message, j.Error.Type))
	}

	return b.String()
}

func formatThroughput(s metrics.ThroughputStats, unit string) string {
	if s.Samples == 0 {
		return dimStyle.Render("n/a")
	}
	return fmt.Sprintf("%.1f %s median (±%.1f, p95: %.1f, n=%d)",
		s.Median, unit, s.StdDev, s.P95, s.Samples)
}

func countStatuses(jobs []job.JobResult) (ok, failed, skipped int) {
	for _, j := range jobs {
		switch j.Status {
		case job.JobStatusOK:
			ok++
		case job.JobStatusFailed:
			failed++
		case job.JobStatusSkipped:
			skipped++
		}
	}
	return
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
