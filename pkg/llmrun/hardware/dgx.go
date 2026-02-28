package hardware

import (
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

// DGX Spark hardware identifiers.
const (
	graceCPUSubstring = "Grace"
	gb10GPUSubstring  = "GB10"
)

// IsDGXSpark detects whether the hardware is an NVIDIA DGX Spark system
// by checking for a Grace CPU and a GB10 GPU (unified memory architecture).
func IsDGXSpark(hw *HardwareInfo) bool {
	if hw == nil {
		return false
	}

	hasGrace := strings.Contains(hw.CPUName, graceCPUSubstring)
	hasGB10 := false
	for _, gpu := range hw.GPUs {
		if strings.Contains(gpu.Name, gb10GPUSubstring) {
			hasGB10 = true
			break
		}
	}

	return hasGrace && hasGB10
}

// ApplyDGXSparkDefaults applies DGX Spark-optimized settings to a RunConfig.
// The DGX Spark GB10 has unified memory (128 GB), a 12-core Grace CPU,
// and supports flash attention on SM_100. These defaults are tuned for
// maximum throughput on this specific hardware.
func ApplyDGXSparkDefaults(cfg *engine.RunConfig) {
	if cfg == nil {
		return
	}

	// Offload all layers to GPU — unified memory makes this efficient.
	cfg.GPULayers = -1

	// NUMA distribute for the Grace CPU's multi-node topology.
	cfg.NumaStrategy = engine.NumaDistribute

	// Flash attention is supported and beneficial on SM_100.
	cfg.FlashAttention = true

	// Lock model in memory — this is a dedicated inference machine.
	cfg.MLock = true
}
