// Package suite orchestrates benchmark runs: parsing configs,
// expanding job matrices, and sequencing execution.
package suite

import (
	"context"
	"fmt"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/job"
	"github.com/lazypower/spark-tools/pkg/llmbench/prompts"
	"github.com/lazypower/spark-tools/pkg/llmbench/store"
	"github.com/lazypower/spark-tools/pkg/llmbench/syscheck"
	"github.com/lazypower/spark-tools/pkg/llmrun"
)

// ProgressFunc reports progress during a benchmark run.
type ProgressFunc func(current, total int, jobID string, status string)

// RunnerOption configures a Runner.
type RunnerOption func(*runnerConfig)

type runnerConfig struct {
	engine     *llmrun.Engine
	store      *store.Store
	outputDir  string
	progressFn ProgressFunc
	skipCheck  bool
	dirtyMode  syscheck.DirtyMode
	continueFrom string
	jobFilter  []string
}

// WithEngine sets the llm-run engine for launching servers.
func WithEngine(eng *llmrun.Engine) RunnerOption {
	return func(c *runnerConfig) { c.engine = eng }
}

// WithStore sets the result store for incremental saves.
func WithStore(s *store.Store) RunnerOption {
	return func(c *runnerConfig) { c.store = s }
}

// WithOutputDir overrides the output directory.
func WithOutputDir(dir string) RunnerOption {
	return func(c *runnerConfig) { c.outputDir = dir }
}

// WithProgressFunc sets a callback for progress updates.
func WithProgressFunc(fn ProgressFunc) RunnerOption {
	return func(c *runnerConfig) { c.progressFn = fn }
}

// WithSkipCheck disables pre-flight system checks.
func WithSkipCheck(skip bool) RunnerOption {
	return func(c *runnerConfig) { c.skipCheck = skip }
}

// WithDirtyMode sets the pre-flight check behavior.
func WithDirtyMode(mode syscheck.DirtyMode) RunnerOption {
	return func(c *runnerConfig) { c.dirtyMode = mode }
}

// WithContinueFrom resumes a run from the given job ID.
func WithContinueFrom(jobID string) RunnerOption {
	return func(c *runnerConfig) { c.continueFrom = jobID }
}

// WithJobFilter filters jobs by pattern.
func WithJobFilter(patterns []string) RunnerOption {
	return func(c *runnerConfig) { c.jobFilter = patterns }
}

// Runner executes benchmark suites.
type Runner struct {
	cfg runnerConfig
}

// NewRunner creates a runner with the given options.
func NewRunner(opts ...RunnerOption) *Runner {
	cfg := runnerConfig{
		dirtyMode: syscheck.DirtyModeAbort,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return &Runner{cfg: cfg}
}

// Run executes all jobs in the suite, returning the complete results.
func (r *Runner) Run(ctx context.Context, s *BenchmarkSuite) (*store.RunResult, error) {
	if r.cfg.engine == nil {
		return nil, fmt.Errorf("engine is required (use WithEngine)")
	}

	// Generate run ID
	runID := store.GenerateRunID()

	// Expand job matrix
	jobs := ExpandJobs(s)
	if len(r.cfg.jobFilter) > 0 {
		jobs = FilterJobs(jobs, r.cfg.jobFilter)
	}

	if len(jobs) == 0 {
		return nil, fmt.Errorf("no jobs to run after filtering")
	}

	// Validate --continue-from up front: an ID that matches no job would
	// otherwise silently skip every job and report an all-skipped run.
	if r.cfg.continueFrom != "" && !continueFromMatches(jobs, r.cfg.continueFrom) {
		return nil, fmt.Errorf("--continue-from job ID %q not found in the suite (%d jobs)", r.cfg.continueFrom, len(jobs))
	}

	// Pre-flight checks
	if !r.cfg.skipCheck {
		dirtyMode := r.cfg.dirtyMode
		if dm := s.Settings.DirtyMode; dm != "" {
			parsed, _ := syscheck.ParseDirtyMode(dm)
			dirtyMode = parsed
		}
		pfResult := syscheck.RunPreflight(ctx, dirtyMode, r.cfg.outputDir)
		if !pfResult.Passed {
			return nil, fmt.Errorf("pre-flight checks failed: %s", pfResult.Results[0].Message)
		}
	}

	// Build result
	result := &store.RunResult{
		SchemaVersion: 1,
		RunID:         runID,
		SuiteName:     s.Name,
		DirtyMode:     s.Settings.DirtyMode,
		StartedAt:     time.Now(),
		Jobs:          make([]job.JobResult, 0, len(jobs)),
	}

	if hw := r.cfg.engine.Hardware(); hw != nil {
		result.Hardware = *hw
	}

	// Save system info early
	if r.cfg.store != nil {
		r.cfg.store.SaveSystem(runID, result.Hardware)
	}

	// Create executor
	executor := job.NewExecutor(
		r.cfg.engine,
		s.Settings.MetricsSampleMs,
		s.Settings.ServerStartupTimeout.Duration,
	)

	// Handle --continue-from
	skipUntil := r.cfg.continueFrom
	skipping := skipUntil != ""

	// Execute jobs
loop:
	for i, js := range jobs {
		select {
		case <-ctx.Done():
			break loop // finalize below persists the partial run
		default:
		}

		// Skip jobs until we reach continue-from
		if skipping {
			if js.JobID == skipUntil {
				skipping = false
			} else {
				skipped := job.JobResult{
					SchemaVersion: 1,
					JobID:         js.JobID,
					ScenarioName:  js.Scenario.Name,
					ScenarioID:    js.ScenarioID,
					RunIndex:      js.RunIndex,
					Status:        job.JobStatusSkipped,
				}
				result.Jobs = append(result.Jobs, skipped)
				continue
			}
		}

		if r.cfg.progressFn != nil {
			r.cfg.progressFn(i+1, len(jobs), js.JobID, "running")
		}

		// Resolve prompts for this job
		promptList, err := r.resolvePrompts(js.Scenario.Prompts)
		if err != nil {
			jr := job.JobResult{
				SchemaVersion: 1,
				JobID:         js.JobID,
				Status:        job.JobStatusFailed,
				Error:         &job.JobError{Type: "prompt_resolve", Message: err.Error()},
			}
			result.Jobs = append(result.Jobs, jr)
			if s.Settings.AbortOnError {
				break
			}
			continue
		}

		// Execute the job
		params := job.ExecParams{
			JobID:          js.JobID,
			ScenarioName:   js.Scenario.Name,
			ScenarioID:     js.ScenarioID,
			RunIndex:       js.RunIndex,
			ModelRef:       fmt.Sprintf("%s:%s", js.ModelSpec.Ref, js.Quant),
			ContextSize:    js.ContextSize,
			BatchSize:      js.BatchSize,
			ParallelSlots:  js.ParallelSlots,
			MaxTokens:      js.MaxTokens,
			Temperature:    js.Temperature,
			WarmupPrompts:  js.WarmupPrompts,
			MeasurePrompts: js.MeasurePrompts,
			Timeout:        js.Timeout.Duration,
		}
		jr := executor.Execute(ctx, params, promptList)
		result.Jobs = append(result.Jobs, jr)

		// Incremental save
		if r.cfg.store != nil {
			r.cfg.store.SaveJob(runID, jr)
		}

		if r.cfg.progressFn != nil {
			r.cfg.progressFn(i+1, len(jobs), js.JobID, string(jr.Status))
		}

		if jr.Status == job.JobStatusFailed && s.Settings.AbortOnError {
			break
		}

		// Cooldown between jobs
		if i < len(jobs)-1 && js.CooldownSecs > 0 {
			job.Cooldown(ctx, js.CooldownSecs)
		}
	}

	// A cancellation detected anywhere — including inside the final job's
	// Execute, which the loop-top check would miss — marks the run interrupted
	// so it is not persisted as complete.
	if ctx.Err() != nil {
		result.Interrupted = true
	}
	result.CompletedAt = time.Now()

	// Persist the run (partial or complete). Surface a save failure (full disk,
	// unwritable dir) rather than reporting success on a run that was never
	// written — even on the interrupt path, where the partial results would
	// otherwise be silently lost.
	if r.cfg.store != nil {
		if err := r.cfg.store.SaveRun(result); err != nil {
			return result, fmt.Errorf("saving run results: %w", err)
		}
	}

	return result, ctx.Err()
}

// continueFromMatches reports whether jobID names a job in the expanded set.
// A --continue-from that matches nothing must be rejected, not treated as
// "skip everything".
func continueFromMatches(jobs []JobSpec, jobID string) bool {
	for _, js := range jobs {
		if js.JobID == jobID {
			return true
		}
	}
	return false
}

// DryRun expands the job matrix and returns the job specs without executing.
func (r *Runner) DryRun(s *BenchmarkSuite) []JobSpec {
	jobs := ExpandJobs(s)
	if len(r.cfg.jobFilter) > 0 {
		jobs = FilterJobs(jobs, r.cfg.jobFilter)
	}
	return jobs
}

func (r *Runner) resolvePrompts(ps PromptSet) ([]string, error) {
	if ps.Builtin != "" {
		return prompts.LoadBuiltin(ps.Builtin)
	}
	if ps.File != "" {
		return prompts.LoadFile(ps.File)
	}
	if len(ps.Inline) > 0 {
		return ps.Inline, nil
	}
	return nil, fmt.Errorf("no prompt source specified")
}
