// Package job executes individual benchmark jobs: server launch,
// warmup, measurement, and teardown.
package job

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmrun"
)

// JobStatus represents the outcome of a benchmark job.
type JobStatus string

const (
	JobStatusOK      JobStatus = "ok"
	JobStatusFailed  JobStatus = "failed"
	JobStatusSkipped JobStatus = "skipped"
)

// JobError captures typed failure information for reporting and resume.
type JobError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Duration wraps time.Duration with JSON string marshaling.
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// JobResult holds the complete output of a single benchmark job.
type JobResult struct {
	SchemaVersion int    `json:"schema_version"`
	JobID         string `json:"job_id"`
	ScenarioName  string `json:"scenario_name"`
	ScenarioID    string `json:"scenario_id"`
	RunIndex      int    `json:"run_index"`

	// Provenance
	Model llmrun.ResolvedModel `json:"model"`

	// Effective launch config
	EffectiveConfig llmrun.RunConfig    `json:"effective_config"`
	Capabilities    llmrun.Capabilities `json:"capabilities"`
	EffectiveFlags  []string            `json:"effective_flags"`
	FlagWarnings    []string            `json:"flag_warnings"`

	// Status
	Status JobStatus `json:"status"`
	Error  *JobError `json:"error"`

	// Timing results
	ModelLoadTimeMs float64                 `json:"model_load_time_ms"`
	PromptEval      metrics.ThroughputStats `json:"prompt_eval_tok_s"`
	Generation      metrics.ThroughputStats `json:"generation_tok_s"`
	FirstTokenTime  metrics.ThroughputStats `json:"ttft_ms"`
	EndToEnd        metrics.ThroughputStats `json:"end_to_end_ms"`

	// System metrics
	SystemMetrics *metrics.SystemMetrics `json:"system_metrics"`

	// Metadata
	Hardware  llmrun.HardwareInfo `json:"hardware"`
	Timestamp time.Time           `json:"timestamp"`
	Duration  Duration            `json:"duration"`

	// Raw samples for downstream analysis
	RawSamples []metrics.RawSample `json:"raw_samples,omitempty"`
}
