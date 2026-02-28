package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
)

func TestJobResult_JSONRoundTrip(t *testing.T) {
	result := JobResult{
		SchemaVersion: 1,
		JobID:         "qwen-32b-Q4_K_M-throughput-1",
		ScenarioName:  "throughput",
		ScenarioID:    "a3f8c2d1e4b5",
		RunIndex:      1,
		Status:        JobStatusOK,
		PromptEval: metrics.ThroughputStats{
			Mean:    1842.3,
			Median:  1838.0,
			P5:      1801.2,
			P95:     1889.4,
			StdDev:  23.1,
			Min:     1790.0,
			Max:     1901.2,
			Samples: 10,
		},
		Generation: metrics.ThroughputStats{
			Mean:    48.7,
			Median:  48.9,
			Samples: 10,
		},
		Timestamp: time.Date(2026, 2, 27, 23, 30, 0, 0, time.UTC),
		Duration:  Duration{Duration: 28*time.Minute + 42*time.Second},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded JobResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.JobID != result.JobID {
		t.Errorf("job_id: got %q, want %q", decoded.JobID, result.JobID)
	}
	if decoded.Status != JobStatusOK {
		t.Errorf("status: got %q, want %q", decoded.Status, JobStatusOK)
	}
	if decoded.PromptEval.Mean != 1842.3 {
		t.Errorf("prompt_eval.mean: got %f, want 1842.3", decoded.PromptEval.Mean)
	}
	if decoded.Generation.Median != 48.9 {
		t.Errorf("generation.median: got %f, want 48.9", decoded.Generation.Median)
	}
	if decoded.Duration.Duration != 28*time.Minute+42*time.Second {
		t.Errorf("duration: got %v", decoded.Duration.Duration)
	}
}

func TestJobResult_FailedStatus(t *testing.T) {
	result := JobResult{
		SchemaVersion: 1,
		JobID:         "test-failed-1",
		Status:        JobStatusFailed,
		Error: &JobError{
			Type:    "timeout",
			Message: "job exceeded 5m timeout",
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded JobResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Status != JobStatusFailed {
		t.Errorf("status: got %q, want %q", decoded.Status, JobStatusFailed)
	}
	if decoded.Error == nil {
		t.Fatal("error should not be nil")
	}
	if decoded.Error.Type != "timeout" {
		t.Errorf("error.type: got %q, want %q", decoded.Error.Type, "timeout")
	}
}

func TestJobStatus_Values(t *testing.T) {
	if JobStatusOK != "ok" {
		t.Errorf("JobStatusOK: got %q", JobStatusOK)
	}
	if JobStatusFailed != "failed" {
		t.Errorf("JobStatusFailed: got %q", JobStatusFailed)
	}
	if JobStatusSkipped != "skipped" {
		t.Errorf("JobStatusSkipped: got %q", JobStatusSkipped)
	}
}
