// Package job executes individual benchmark jobs: server launch,
// warmup, measurement, and teardown.
package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
	"github.com/lazypower/spark-tools/pkg/llmrun"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

// HFClient is the interface for model downloading.
type HFClient interface {
	Pull(ctx context.Context, modelID, filename string, opts interface{}) error
}

// ExecParams holds the parameters needed to execute a single benchmark job.
// This avoids a dependency cycle between job and suite packages.
type ExecParams struct {
	JobID          string
	ScenarioName   string
	ScenarioID     string
	RunIndex       int
	ModelRef       string  // e.g. "bartowski/model:Q4_K_M"
	ContextSize    int
	BatchSize      int
	ParallelSlots  int
	MaxTokens      int
	Temperature    float64
	WarmupPrompts  int
	MeasurePrompts int
	// Timeout bounds a single request. A server that accepts the connection but
	// stops streaming would otherwise hang an unattended run forever. Zero means
	// no per-request bound.
	Timeout time.Duration
}

// Executor runs individual benchmark jobs.
type Executor struct {
	engine          *llmrun.Engine
	metricsSampleMs int
	startupTimeout  time.Duration
}

// NewExecutor creates a job executor.
func NewExecutor(eng *llmrun.Engine, metricsSampleMs int, startupTimeout time.Duration) *Executor {
	if metricsSampleMs <= 0 {
		metricsSampleMs = 500
	}
	if startupTimeout <= 0 {
		startupTimeout = 2 * time.Minute
	}
	return &Executor{
		engine:          eng,
		metricsSampleMs: metricsSampleMs,
		startupTimeout:  startupTimeout,
	}
}

// Execute runs a single benchmark job. It never returns an error —
// failures are captured in the returned JobResult.
func (e *Executor) Execute(ctx context.Context, params ExecParams, prompts []string) JobResult {
	start := time.Now()
	result := JobResult{
		SchemaVersion: 1,
		JobID:         params.JobID,
		ScenarioName:  params.ScenarioName,
		ScenarioID:    params.ScenarioID,
		RunIndex:      params.RunIndex,
		Timestamp:     start,
	}

	// Capture hardware info
	if hw := e.engine.Hardware(); hw != nil {
		result.Hardware = *hw
	}

	// Build RunConfig
	cfg := llmrun.RunConfig{
		ModelRef:    params.ModelRef,
		ServerMode:  true,
		ContextSize: params.ContextSize,
		BatchSize:   params.BatchSize,
		Temperature: params.Temperature,
		GPULayers:   -1, // Offload all
	}
	if params.ParallelSlots > 1 {
		cfg.Parallel = params.ParallelSlots
	}

	// Resolve model
	resolved, err := e.engine.ResolveModel(ctx, cfg.ModelRef)
	if err != nil {
		result.Status = JobStatusFailed
		result.Error = &JobError{Type: "model_resolve", Message: err.Error()}
		result.Duration = Duration{time.Since(start)}
		return result
	}
	result.Model = *resolved
	cfg.ModelPath = resolved.Path

	// Build command for flags recording
	caps := e.engine.DetectCapabilities()
	result.Capabilities = *caps
	result.EffectiveConfig = cfg
	cmd, warnings, _ := llmrun.BuildCommand(cfg, *caps)
	result.EffectiveFlags = cmd
	result.FlagWarnings = warnings

	// Launch server
	proc, err := e.engine.Launch(ctx, cfg)
	if err != nil {
		result.Status = JobStatusFailed
		result.Error = &JobError{Type: "server_start", Message: err.Error()}
		result.Duration = Duration{time.Since(start)}
		return result
	}
	defer func() {
		proc.Stop()
	}()

	// Wait for server ready, cancelling early if the process dies.
	readyCtx, readyCancel := context.WithTimeout(ctx, e.startupTimeout)
	go func() {
		select {
		case <-proc.Done():
			readyCancel()
		case <-readyCtx.Done():
		}
	}()

	if err := engine.WaitForReady(readyCtx, proc.Endpoint, e.startupTimeout); err != nil {
		readyCancel()
		msg := fmt.Sprintf("server not ready: %v", err)
		if proc.Err() != nil {
			crashLog := proc.CrashLog(4096)
			if crashLog != "" {
				msg = fmt.Sprintf("server crashed during startup: %v\n%s", proc.Err(), crashLog)
			} else {
				msg = fmt.Sprintf("server crashed during startup: %v (log: %s)", proc.Err(), proc.LogFile)
			}
		}
		result.Status = JobStatusFailed
		result.Error = &JobError{Type: "server_start", Message: msg}
		result.Duration = Duration{time.Since(start)}
		return result
	}
	readyCancel()
	result.ModelLoadTimeMs = float64(time.Since(start).Milliseconds())

	endpoint := proc.Endpoint

	// Warmup
	if params.WarmupPrompts > 0 {
		if err := sendWarmupPrompts(ctx, endpoint, prompts, params.WarmupPrompts, params.MaxTokens, params.Temperature, params.Timeout); err != nil {
			result.Status = JobStatusFailed
			result.Error = &JobError{Type: "warmup", Message: err.Error()}
			result.Duration = Duration{time.Since(start)}
			return result
		}
	}

	// Start system metrics sampling
	sampler := metrics.NewSystemSampler(e.metricsSampleMs)
	sampler.Start(ctx)

	// Measurement phase
	collector := metrics.NewCollector()
	measurePrompts := params.MeasurePrompts
	if measurePrompts > len(prompts) {
		measurePrompts = len(prompts)
	}

	if params.ParallelSlots > 1 {
		// Parallel load testing. A probe failure is a real result, not something
		// to hide behind an OK status with fewer samples.
		_, failures, firstErr := measureParallel(ctx, endpoint, prompts, measurePrompts, params.ParallelSlots, params.MaxTokens, params.Temperature, params.Timeout, collector)
		if failures > 0 {
			result.Status = JobStatusFailed
			result.Error = &JobError{
				Type:    probeErrorType(firstErr),
				Message: fmt.Sprintf("%d parallel probe(s) failed: %v", failures, firstErr),
			}
			result.Duration = Duration{time.Since(start)}
			result.SystemMetrics = sampler.Stop()
			return result
		}
	} else {
		// Sequential measurement
		for i := 0; i < measurePrompts; i++ {
			select {
			case <-ctx.Done():
				result.Status = JobStatusFailed
				result.Error = &JobError{Type: "timeout", Message: ctx.Err().Error()}
				result.Duration = Duration{time.Since(start)}
				result.SystemMetrics = sampler.Stop()
				return result
			default:
			}

			promptIdx := i % len(prompts)
			pr := probeRequest(ctx, endpoint, prompts[promptIdx], params.MaxTokens, params.Temperature, params.Timeout)
			if pr.Err != nil {
				result.Status = JobStatusFailed
				result.Error = &JobError{Type: probeErrorType(pr.Err), Message: pr.Err.Error()}
				result.Duration = Duration{time.Since(start)}
				result.SystemMetrics = sampler.Stop()
				return result
			}
			collector.Add(pr.Sample)
		}
	}

	// Stop system sampling and collect
	result.SystemMetrics = sampler.Stop()

	// Aggregate metrics
	collected := collector.Collect()
	result.PromptEval = collected.PromptEval
	result.Generation = collected.Generation
	result.FirstTokenTime = collected.FirstTokenTime
	result.EndToEnd = collected.EndToEnd
	result.RawSamples = collected.RawSamples

	result.Status = JobStatusOK
	result.Duration = Duration{time.Since(start)}
	return result
}

// probeErrorType classifies a probe failure: a deadline (the per-request
// timeout firing on a stalled server) is a "timeout", everything else "probe".
func probeErrorType(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "probe"
}

// measureParallel runs the probes across slots and returns how many succeeded,
// how many failed, and the first failure. Failures must be surfaced by the
// caller — dropping them and reporting an OK job with fewer samples would
// misrepresent a run where requests timed out or errored.
func measureParallel(ctx context.Context, endpoint string, prompts []string, totalPrompts, slots int, maxTokens int, temperature float64, timeout time.Duration, collector *metrics.Collector) (collected, failures int, firstErr error) {
	promptsPerSlot := totalPrompts / slots
	if promptsPerSlot < 1 {
		promptsPerSlot = 1
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results []probeResult
	)

	for slot := 0; slot < slots; slot++ {
		wg.Add(1)
		slotOffset := slot * promptsPerSlot
		go func(slotIdx, offset int) {
			defer wg.Done()
			// Stagger start by 100ms per slot
			time.Sleep(time.Duration(slotIdx*100) * time.Millisecond)

			for i := 0; i < promptsPerSlot; i++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				promptIdx := (offset + i) % len(prompts)
				pr := probeRequest(ctx, endpoint, prompts[promptIdx], maxTokens, temperature, timeout)
				mu.Lock()
				results = append(results, pr)
				mu.Unlock()
			}
		}(slot, slotOffset)
	}

	wg.Wait()

	for _, pr := range results {
		if pr.Err != nil {
			failures++
			if firstErr == nil {
				firstErr = pr.Err
			}
			continue
		}
		collector.Add(pr.Sample)
		collected++
	}
	return collected, failures, firstErr
}
