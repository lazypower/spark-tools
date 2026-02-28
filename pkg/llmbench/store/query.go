package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
)

// List returns summaries of stored runs matching the filter.
func (s *Store) List(filter StoreFilter) ([]RunSummary, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading results directory: %w", err)
	}

	var summaries []RunSummary
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "run-") {
			continue
		}

		summary, err := s.loadSummary(e.Name())
		if err != nil {
			continue // Skip unreadable runs
		}

		// Apply filters
		if !filter.After.IsZero() && summary.StartedAt.Before(filter.After) {
			continue
		}
		if !filter.Before.IsZero() && summary.StartedAt.After(filter.Before) {
			continue
		}
		if filter.Model != "" {
			// Check if any job in the run matches the model pattern
			if !s.runMatchesModel(e.Name(), filter.Model) {
				continue
			}
		}

		summaries = append(summaries, *summary)
	}

	// Sort by start time, newest first
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartedAt.After(summaries[j].StartedAt)
	})

	return summaries, nil
}

// Load reads the complete results for a run.
func (s *Store) Load(runID string) (*RunResult, error) {
	dir := s.RunDir(runID)
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		return nil, fmt.Errorf("reading results for %s: %w", runID, err)
	}
	var result RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing results for %s: %w", runID, err)
	}
	return &result, nil
}

// LoadJob reads a single job result from a run.
func (s *Store) LoadJob(runID, jobID string) (*job.JobResult, error) {
	path := filepath.Join(s.RunDir(runID), "jobs", jobID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading job %s/%s: %w", runID, jobID, err)
	}
	var result job.JobResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing job %s/%s: %w", runID, jobID, err)
	}
	return &result, nil
}

func (s *Store) loadSummary(runID string) (*RunSummary, error) {
	// Try summary.json first
	path := filepath.Join(s.RunDir(runID), "summary.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var summary RunSummary
		if err := json.Unmarshal(data, &summary); err == nil {
			return &summary, nil
		}
	}

	// Fall back to results.json
	result, err := s.Load(runID)
	if err != nil {
		return nil, err
	}
	return &RunSummary{
		RunID:       result.RunID,
		SuiteName:   result.SuiteName,
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
		JobCount:    len(result.Jobs),
		FailedCount: countFailed(result.Jobs),
	}, nil
}

func (s *Store) runMatchesModel(runID, pattern string) bool {
	result, err := s.Load(runID)
	if err != nil {
		return false
	}
	for _, j := range result.Jobs {
		if strings.Contains(j.JobID, pattern) ||
			strings.Contains(j.Model.RequestedRef, pattern) ||
			strings.Contains(j.Model.NormalizedRef, pattern) {
			return true
		}
	}
	return false
}
