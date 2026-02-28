package hardware

import (
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

// RecommendConfig generates a RunConfig with smart defaults based on the
// detected hardware and parsed GGUF model metadata. The returned config
// sets thread count, GPU offload, context size, batch size, and memory
// management flags to reasonable values for the hardware/model combination.
func RecommendConfig(hw *HardwareInfo, meta *gguf.GGUFMetadata) engine.RunConfig {
	cfg := engine.RunConfig{
		FlashAttention: true,
		MMap:           true,
		MLock:          false,
	}

	if hw == nil {
		// No hardware info; return conservative defaults.
		cfg.Threads = 1
		cfg.GPULayers = 0
		cfg.ContextSize = 2048
		cfg.BatchSize = 512
		return cfg
	}

	// Thread count: leave 2 cores for OS/system overhead, minimum 1.
	cfg.Threads = hw.CPUCores - 2
	if cfg.Threads < 1 {
		cfg.Threads = 1
	}

	// GPU offload: all layers if any GPU is available, CPU-only otherwise.
	if len(hw.GPUs) > 0 {
		cfg.GPULayers = -1
	} else {
		cfg.GPULayers = 0
	}

	// Context size: estimate maximum based on available memory after model load.
	cfg.ContextSize = estimateMaxContext(hw, meta)

	// Batch size: scale with available memory.
	cfg.BatchSize = recommendBatchSize(hw, meta)

	// MLock on DGX Spark or machines with substantial memory.
	if hw.IsDGXSpark {
		cfg.MLock = true
	}

	// Apply DGX Spark overrides if applicable.
	if hw.IsDGXSpark {
		ApplyDGXSparkDefaults(&cfg)
	}

	return cfg
}

// estimateMaxContext calculates the maximum context length the hardware
// can support for a given model, based on available memory after the model
// weights are loaded.
//
// KV cache per token = 2 (K+V) * layers * embedding_dim * 2 bytes (FP16)
//
// Available memory for KV cache = (totalMem - modelSize) * 0.9 (90% headroom)
//
// The result is capped at the model's trained context length (if known).
func estimateMaxContext(hw *HardwareInfo, meta *gguf.GGUFMetadata) int {
	const (
		defaultContext = 4096
		minContext     = 512
	)

	if hw == nil {
		return defaultContext
	}

	if meta == nil || meta.LayerCount == 0 || meta.EmbeddingSize == 0 {
		// Without model metadata, fall back to a heuristic based on
		// available memory: ~4K context per 8 GB, capped at 32K.
		ctx := int(hw.TotalMemoryGB/8) * 4096
		if ctx < defaultContext {
			ctx = defaultContext
		}
		if ctx > 32768 {
			ctx = 32768
		}
		return ctx
	}

	// Estimate model file size from parameter count and quantization.
	modelSizeGB := estimateModelSizeGB(meta)

	// Available memory after model load (90% of remaining).
	availableGB := (hw.TotalMemoryGB - modelSizeGB) * 0.9
	if availableGB < 0 {
		availableGB = 0
	}

	// KV cache bytes per token:
	// 2 (K+V) * layers * embedding_dim * 2 bytes (FP16)
	kvBytesPerToken := float64(2) * float64(meta.LayerCount) * float64(meta.EmbeddingSize) * 2.0

	if kvBytesPerToken <= 0 {
		return defaultContext
	}

	// Convert available GB to bytes.
	availableBytes := availableGB * 1024 * 1024 * 1024

	maxTokens := int(availableBytes / kvBytesPerToken)

	// Cap at model's trained context length if known.
	if meta.ContextLength > 0 && maxTokens > meta.ContextLength {
		maxTokens = meta.ContextLength
	}

	// Enforce minimum.
	if maxTokens < minContext {
		maxTokens = minContext
	}

	// Round down to nearest power-of-2-friendly number for llama.cpp.
	maxTokens = roundDownContext(maxTokens)

	return maxTokens
}

// recommendBatchSize returns an appropriate batch size for prompt processing
// based on available memory and model characteristics. Larger batch sizes
// improve prompt processing throughput but use more memory.
func recommendBatchSize(hw *HardwareInfo, meta *gguf.GGUFMetadata) int {
	const defaultBatch = 512

	if hw == nil {
		return defaultBatch
	}

	// Scale batch size with total memory:
	//   < 16 GB  -> 256
	//   16-64 GB -> 512
	//   64+ GB   -> 2048
	switch {
	case hw.TotalMemoryGB >= 64:
		return 2048
	case hw.TotalMemoryGB >= 16:
		return 512
	default:
		return 256
	}
}

// estimateModelSizeGB estimates the on-disk/in-memory size of a model
// based on parameter count and quantization bits per weight.
func estimateModelSizeGB(meta *gguf.GGUFMetadata) float64 {
	if meta == nil || meta.ParameterCount == 0 {
		return 0
	}

	bpw := 4.85 // default: Q4_K_M as a reasonable guess
	if meta.QuantType != "" {
		if v, ok := gguf.QuantBitsPerWeight[meta.QuantType]; ok {
			bpw = v
		}
	}

	// Size in bytes = parameters * bits_per_weight / 8
	sizeBytes := float64(meta.ParameterCount) * bpw / 8.0
	return sizeBytes / (1024 * 1024 * 1024)
}

// roundDownContext rounds a context length down to a "clean" value
// that llama.cpp handles well. We use multiples of 256 up to 32K,
// then multiples of 1024 above that.
func roundDownContext(ctx int) int {
	if ctx <= 0 {
		return 512
	}
	if ctx >= 32768 {
		return (ctx / 1024) * 1024
	}
	return (ctx / 256) * 256
}
