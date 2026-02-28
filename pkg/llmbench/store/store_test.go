package store

import (
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
)

func testRunResult() *RunResult {
	return &RunResult{
		SchemaVersion: 1,
		RunID:         "run-20260227-233000",
		SuiteName:     "Test Suite",
		DirtyMode:     "abort",
		StartedAt:     time.Date(2026, 2, 27, 23, 30, 0, 0, time.UTC),
		CompletedAt:   time.Date(2026, 2, 27, 23, 58, 42, 0, time.UTC),
		Jobs: []job.JobResult{
			{
				SchemaVersion: 1,
				JobID:         "model-a-Q4_K_M-throughput-1",
				ScenarioName:  "throughput",
				ScenarioID:    "abc123def456",
				RunIndex:      1,
				Status:        job.JobStatusOK,
				PromptEval: metrics.ThroughputStats{
					Mean:    1842.3,
					Median:  1838.0,
					Samples: 10,
				},
				Generation: metrics.ThroughputStats{
					Mean:    48.7,
					Median:  48.9,
					Samples: 10,
				},
			},
			{
				SchemaVersion: 1,
				JobID:         "model-a-Q4_K_M-throughput-2",
				ScenarioName:  "throughput",
				RunIndex:      2,
				Status:        job.JobStatusFailed,
				Error: &job.JobError{
					Type:    "timeout",
					Message: "exceeded 5m",
				},
			},
		},
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	result := testRunResult()

	if err := s.SaveRun(result); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	loaded, err := s.Load(result.RunID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.RunID != result.RunID {
		t.Errorf("run_id: got %q, want %q", loaded.RunID, result.RunID)
	}
	if loaded.SuiteName != result.SuiteName {
		t.Errorf("suite_name: got %q", loaded.SuiteName)
	}
	if len(loaded.Jobs) != 2 {
		t.Fatalf("jobs: got %d, want 2", len(loaded.Jobs))
	}
	if loaded.Jobs[0].PromptEval.Mean != 1842.3 {
		t.Errorf("job[0] prompt_eval.mean: got %f", loaded.Jobs[0].PromptEval.Mean)
	}
	if loaded.Jobs[1].Status != job.JobStatusFailed {
		t.Errorf("job[1] status: got %q", loaded.Jobs[1].Status)
	}
}

func TestSaveJob_Incremental(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	runID := "run-20260227-120000"

	j := job.JobResult{
		SchemaVersion: 1,
		JobID:         "test-job-1",
		Status:        job.JobStatusOK,
	}

	if err := s.SaveJob(runID, j); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	loaded, err := s.LoadJob(runID, "test-job-1")
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	if loaded.JobID != "test-job-1" {
		t.Errorf("job_id: got %q", loaded.JobID)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Save two runs
	r1 := testRunResult()
	r1.RunID = "run-20260227-100000"
	r1.StartedAt = time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	s.SaveRun(r1)

	r2 := testRunResult()
	r2.RunID = "run-20260227-120000"
	r2.SuiteName = "Another Suite"
	r2.StartedAt = time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	s.SaveRun(r2)

	// List all
	summaries, err := s.List(StoreFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	// Should be newest first
	if summaries[0].RunID != "run-20260227-120000" {
		t.Errorf("first should be newest, got %q", summaries[0].RunID)
	}

	// Filter by date
	after := time.Date(2026, 2, 27, 11, 0, 0, 0, time.UTC)
	summaries, err = s.List(StoreFilter{After: after})
	if err != nil {
		t.Fatalf("List with filter: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
}

func TestList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	summaries, err := s.List(StoreFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("got %d summaries, want 0", len(summaries))
	}
}

func TestGenerateRunID(t *testing.T) {
	id := GenerateRunID()
	if len(id) < 19 {
		t.Errorf("run ID too short: %q", id)
	}
	if id[:4] != "run-" {
		t.Errorf("run ID should start with 'run-': %q", id)
	}
}
