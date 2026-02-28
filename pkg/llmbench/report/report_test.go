package report

import (
	"strings"
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func testRunResult() *store.RunResult {
	return &store.RunResult{
		SchemaVersion: 1,
		RunID:         "run-20260227-233000",
		SuiteName:     "Test Suite",
		StartedAt:     time.Date(2026, 2, 27, 23, 30, 0, 0, time.UTC),
		CompletedAt:   time.Date(2026, 2, 27, 23, 58, 42, 0, time.UTC),
		Jobs: []job.JobResult{
			{
				JobID:        "model-a-Q4_K_M-throughput-1",
				ScenarioName: "throughput",
				Status:       job.JobStatusOK,
				Model: resolver.ResolvedModel{
					NormalizedRef: "owner/model-a:Q4_K_M",
					Quant:         "Q4_K_M",
				},
				PromptEval: metrics.ThroughputStats{
					Mean: 1842.3, Median: 1838.0, StdDev: 23.1,
					P5: 1801.2, P95: 1889.4, Min: 1790, Max: 1901, Samples: 10,
				},
				Generation: metrics.ThroughputStats{
					Mean: 48.7, Median: 48.9, StdDev: 1.2,
					P5: 46.2, P95: 50.1, Min: 45.8, Max: 51.3, Samples: 10,
				},
				FirstTokenTime: metrics.ThroughputStats{
					Mean: 131.2, Median: 127.0, P95: 203.0, Samples: 10,
				},
				ModelLoadTimeMs: 4200,
			},
			{
				JobID:        "model-a-Q4_K_M-throughput-2",
				ScenarioName: "throughput",
				Status:       job.JobStatusFailed,
				Error: &job.JobError{
					Type:    "timeout",
					Message: "exceeded 5m timeout",
				},
			},
		},
	}
}

func TestTerminal(t *testing.T) {
	result := testRunResult()
	output := Terminal(result)

	if !strings.Contains(output, "Test Suite") {
		t.Error("should contain suite name")
	}
	if !strings.Contains(output, "run-20260227-233000") {
		t.Error("should contain run ID")
	}
	if !strings.Contains(output, "model-a-Q4_K_M-throughput-1") {
		t.Error("should contain job ID")
	}
	if !strings.Contains(output, "48.9") {
		t.Error("should contain generation median")
	}
	if !strings.Contains(output, "timeout") {
		t.Error("should contain error info")
	}
}

func TestQuickResult_OK(t *testing.T) {
	j := testRunResult().Jobs[0]
	output := QuickResult(j)

	if !strings.Contains(output, "Quick Benchmark") {
		t.Error("should contain Quick Benchmark header")
	}
	if !strings.Contains(output, "Generation") {
		t.Error("should contain Generation label")
	}
}

func TestQuickResult_Failed(t *testing.T) {
	j := testRunResult().Jobs[1]
	output := QuickResult(j)

	if !strings.Contains(output, "Failed") {
		t.Error("should contain Failed")
	}
}

func TestJSON(t *testing.T) {
	result := testRunResult()
	data, err := JSON(result)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("JSON output is empty")
	}
	if !strings.Contains(string(data), "run-20260227-233000") {
		t.Error("JSON should contain run ID")
	}
}

func TestJSONPretty(t *testing.T) {
	result := testRunResult()
	data, err := JSONPretty(result)
	if err != nil {
		t.Fatalf("JSONPretty: %v", err)
	}
	if !strings.Contains(string(data), "\n") {
		t.Error("pretty JSON should be multi-line")
	}
}

func TestCSV(t *testing.T) {
	result := testRunResult()
	data, err := CSV(result)
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 { // header + 2 jobs
		t.Errorf("expected 3 CSV lines, got %d", len(lines))
	}
	// Check header
	if !strings.Contains(lines[0], "job_id") {
		t.Error("CSV header should contain job_id")
	}
	// Check columns count matches
	headerCols := strings.Count(lines[0], ",") + 1
	dataCols := strings.Count(lines[1], ",") + 1
	if headerCols != dataCols {
		t.Errorf("column count mismatch: header=%d, data=%d", headerCols, dataCols)
	}
}

func TestCompare(t *testing.T) {
	r1 := testRunResult()
	output := Compare([]*store.RunResult{r1}, "generation")

	if !strings.Contains(output, "Generation Speed") {
		t.Error("should contain metric title")
	}
	if !strings.Contains(output, "Q4_K_M") {
		t.Error("should contain quant")
	}
}

func TestCompare_NoJobs(t *testing.T) {
	empty := &store.RunResult{}
	output := Compare([]*store.RunResult{empty}, "")
	if !strings.Contains(output, "No successful") {
		t.Error("should show no jobs message")
	}
}
