// Package metrics collects and aggregates benchmark measurements:
// throughput stats, system resource usage, and timing data.
package metrics

// ThroughputStats holds aggregated performance metrics.
type ThroughputStats struct {
	Mean    float64 `json:"mean"`
	Median  float64 `json:"median"`
	P5      float64 `json:"p5"`
	P95     float64 `json:"p95"`
	StdDev  float64 `json:"stddev"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Samples int     `json:"samples"`
}

// SystemMetrics captures resource utilization during the measurement phase.
type SystemMetrics struct {
	Available        bool    `json:"available"`
	PeakMemoryMB     int64   `json:"peak_memory_mb"`
	PeakGPUMemoryMB  int64   `json:"peak_gpu_memory_mb"`
	MeanCPUPercent   float64 `json:"mean_cpu_pct"`
	MeanGPUPercent   float64 `json:"mean_gpu_pct"`
	PeakGPUPercent   float64 `json:"peak_gpu_pct"`
	ThermalThrottled bool    `json:"thermal_throttled"`
	SampleCount      int     `json:"sample_count"`
	SampleIntervalMs int     `json:"sample_interval_ms"`
}

// RawSample holds a single measurement from one prompt request.
type RawSample struct {
	PromptTokens    int     `json:"prompt_tokens"`
	PredictedTokens int     `json:"predicted_tokens"`
	PromptMs        float64 `json:"prompt_ms"`
	PredictedMs     float64 `json:"predicted_ms"`
	TTFTMs          float64 `json:"ttft_ms"`
	EndToEndMs      float64 `json:"end_to_end_ms"`
	PromptBytes     int     `json:"prompt_bytes"`
}

// Timings holds the raw timing data from llama-server's response.
type Timings struct {
	PromptN     int     `json:"prompt_n"`
	PromptMs    float64 `json:"prompt_ms"`
	PredictedN  int     `json:"predicted_n"`
	PredictedMs float64 `json:"predicted_ms"`
}
