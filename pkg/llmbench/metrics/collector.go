// Package metrics collects and aggregates benchmark measurements:
// throughput stats, system resource usage, and timing data.
package metrics

import (
	"encoding/json"
)

// ExtractTimings parses llama-server timings from a raw JSON response body.
// The timings object contains prompt_n, prompt_ms, predicted_n, predicted_ms.
func ExtractTimings(data []byte) (*Timings, error) {
	var envelope struct {
		Timings Timings `json:"timings"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Timings, nil
}

// ComputeRates computes tokens-per-second rates from raw timings.
func ComputeRates(t *Timings) (promptTokPerSec, genTokPerSec float64) {
	if t.PromptMs > 0 {
		promptTokPerSec = float64(t.PromptN) / (t.PromptMs / 1000.0)
	}
	if t.PredictedMs > 0 {
		genTokPerSec = float64(t.PredictedN) / (t.PredictedMs / 1000.0)
	}
	return
}

// Collector accumulates RawSamples and produces aggregated results.
type Collector struct {
	samples []RawSample
}

// NewCollector creates a new Collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Add records a raw sample from a single prompt request.
func (c *Collector) Add(sample RawSample) {
	c.samples = append(c.samples, sample)
}

// Samples returns the raw samples collected so far.
func (c *Collector) Samples() []RawSample {
	return c.samples
}

// Count returns the number of samples collected.
func (c *Collector) Count() int {
	return len(c.samples)
}

// CollectedResults holds the aggregated results from a Collector.
type CollectedResults struct {
	PromptEval     ThroughputStats
	Generation     ThroughputStats
	FirstTokenTime ThroughputStats
	EndToEnd       ThroughputStats
	RawSamples     []RawSample
}

// Collect aggregates all accumulated samples into stats.
func (c *Collector) Collect() CollectedResults {
	var promptRates, genRates, ttfts, e2es []float64

	for _, s := range c.samples {
		if s.PromptMs > 0 && s.PromptTokens > 0 {
			promptRates = append(promptRates, float64(s.PromptTokens)/(s.PromptMs/1000.0))
		}
		if s.PredictedMs > 0 && s.PredictedTokens > 0 {
			genRates = append(genRates, float64(s.PredictedTokens)/(s.PredictedMs/1000.0))
		}
		if s.TTFTMs > 0 {
			ttfts = append(ttfts, s.TTFTMs)
		}
		if s.EndToEndMs > 0 {
			e2es = append(e2es, s.EndToEndMs)
		}
	}

	return CollectedResults{
		PromptEval:     Aggregate(promptRates),
		Generation:     Aggregate(genRates),
		FirstTokenTime: Aggregate(ttfts),
		EndToEnd:       Aggregate(e2es),
		RawSamples:     c.samples,
	}
}
