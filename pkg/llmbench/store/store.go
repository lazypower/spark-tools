// Package store persists and retrieves benchmark results
// with support for historical queries and run comparison.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmrun"

	"gopkg.in/yaml.v3"
)

// Store manages persistent storage of benchmark results.
type Store struct {
	baseDir string // $LLM_BENCH_DATA_DIR/results
}

// NewStore creates a Store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// RunResult holds the complete results of a benchmark run.
type RunResult struct {
	SchemaVersion    int               `json:"schema_version"`
	RunID            string            `json:"run_id"`
	SuiteName        string            `json:"suite_name"`
	DirtyMode        string            `json:"dirty_mode"`
	PreflightWarnings []string         `json:"preflight_warnings"`
	StartedAt        time.Time         `json:"started_at"`
	CompletedAt      time.Time         `json:"completed_at"`
	Hardware         llmrun.HardwareInfo `json:"hardware"`
	Jobs             []job.JobResult   `json:"jobs"`
}

// GenerateRunID creates a run ID in the format run-YYYYMMDD-HHMMSS.
func GenerateRunID() string {
	return fmt.Sprintf("run-%s", time.Now().Format("20060102-150405"))
}

// RunDir returns the directory path for a given run ID.
func (s *Store) RunDir(runID string) string {
	return filepath.Join(s.baseDir, runID)
}

// SaveRun writes the complete run result.
func (s *Store) SaveRun(result *RunResult) error {
	dir := s.RunDir(result.RunID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating run directory: %w", err)
	}

	// Write results.json
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), data, 0644); err != nil {
		return fmt.Errorf("writing results.json: %w", err)
	}

	// Write summary.json
	summary := RunSummary{
		RunID:       result.RunID,
		SuiteName:   result.SuiteName,
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
		JobCount:    len(result.Jobs),
		FailedCount: countFailed(result.Jobs),
	}
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), summaryData, 0644); err != nil {
		return fmt.Errorf("writing summary.json: %w", err)
	}

	// Write system.json
	sysData, err := json.MarshalIndent(result.Hardware, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling system info: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system.json"), sysData, 0644); err != nil {
		return fmt.Errorf("writing system.json: %w", err)
	}

	// Write per-job files
	jobsDir := filepath.Join(dir, "jobs")
	if err := os.MkdirAll(jobsDir, 0755); err != nil {
		return fmt.Errorf("creating jobs directory: %w", err)
	}
	for _, j := range result.Jobs {
		if err := s.saveJobFile(jobsDir, j); err != nil {
			return err
		}
	}

	return nil
}

// SaveConfig saves a copy of the input config.
func (s *Store) SaveConfig(runID string, configData []byte) error {
	dir := s.RunDir(runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.yaml"), configData, 0644)
}

// SaveSystem writes hardware/system info for a run.
func (s *Store) SaveSystem(runID string, hw llmrun.HardwareInfo) error {
	dir := s.RunDir(runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "system.json"), data, 0644)
}

// SaveJob incrementally saves a single job result to the run directory.
func (s *Store) SaveJob(runID string, result job.JobResult) error {
	jobsDir := filepath.Join(s.RunDir(runID), "jobs")
	if err := os.MkdirAll(jobsDir, 0755); err != nil {
		return fmt.Errorf("creating jobs directory: %w", err)
	}
	return s.saveJobFile(jobsDir, result)
}

func (s *Store) saveJobFile(jobsDir string, result job.JobResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling job %s: %w", result.JobID, err)
	}
	path := filepath.Join(jobsDir, result.JobID+".json")
	return os.WriteFile(path, data, 0644)
}

// RunSummary provides a quick overview of a stored run.
type RunSummary struct {
	RunID       string    `json:"run_id"`
	SuiteName   string    `json:"suite_name"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	JobCount    int       `json:"job_count"`
	FailedCount int       `json:"failed_count"`
}

// StoreFilter filters runs during listing.
type StoreFilter struct {
	Model  string
	After  time.Time
	Before time.Time
}

func countFailed(jobs []job.JobResult) int {
	n := 0
	for _, j := range jobs {
		if j.Status == job.JobStatusFailed {
			n++
		}
	}
	return n
}

// configForSave creates a minimal YAML representation of run settings.
func configForSave(result *RunResult) ([]byte, error) {
	cfg := map[string]interface{}{
		"suite_name": result.SuiteName,
		"run_id":     result.RunID,
		"dirty_mode": result.DirtyMode,
		"started_at": result.StartedAt.Format(time.RFC3339),
	}
	return yaml.Marshal(cfg)
}
