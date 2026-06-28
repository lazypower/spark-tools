package main

import (
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
)

// applyDefaults is the flag-precedence seam: explicit user values must survive,
// and only zero/unset fields are filled from the hardware recommendation.
func TestApplyDefaults_UserValuesWin(t *testing.T) {
	rec := engine.RunConfig{
		Threads: 16, GPULayers: 99, ContextSize: 8192, BatchSize: 512,
		Temperature: 0.8, FlashAttention: true, MMap: true, MLock: true,
		NumaStrategy: engine.NumaDistribute,
	}
	cfg := engine.RunConfig{
		Threads: 4, GPULayers: 10, ContextSize: 2048, BatchSize: 128, Temperature: 0.2,
	}
	applyDefaults(&cfg, rec)

	// Explicitly-set numeric flags are untouched.
	if cfg.Threads != 4 || cfg.GPULayers != 10 || cfg.ContextSize != 2048 ||
		cfg.BatchSize != 128 || cfg.Temperature != 0.2 {
		t.Errorf("user-set values must not be overwritten: %+v", cfg)
	}
	// Unset bools still pick up the recommendation.
	if !cfg.FlashAttention || !cfg.MMap || !cfg.MLock {
		t.Errorf("unset bool flags must take the recommendation: %+v", cfg)
	}
	if cfg.NumaStrategy != engine.NumaDistribute {
		t.Errorf("unset numa strategy must take the recommendation, got %v", cfg.NumaStrategy)
	}
}

func TestApplyDefaults_FillsZeroValues(t *testing.T) {
	rec := engine.RunConfig{Threads: 8, GPULayers: 33, ContextSize: 4096, BatchSize: 256, Temperature: 0.7}
	var cfg engine.RunConfig // all zero
	applyDefaults(&cfg, rec)
	if cfg.Threads != 8 || cfg.GPULayers != 33 || cfg.ContextSize != 4096 ||
		cfg.BatchSize != 256 || cfg.Temperature != 0.7 {
		t.Errorf("zero cfg must be filled from recommendation, got %+v", cfg)
	}
}

func TestGPUName(t *testing.T) {
	if gpuName(nil) != "" {
		t.Error("nil hardware must yield empty GPU name")
	}
	if gpuName(&hardware.HardwareInfo{}) != "" {
		t.Error("no GPUs must yield empty GPU name")
	}
	hw := &hardware.HardwareInfo{GPUs: []hardware.GPUInfo{{Name: "GB10"}}}
	if gpuName(hw) != "GB10" {
		t.Errorf("must return the first GPU name, got %q", gpuName(hw))
	}
}
