package hardware

import (
	"github.com/lazypower/spark-tools/internal/gguf"
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

// memoryBudgetGB returns the pool a model's weights and KV cache actually have
// to fit in.
//
// When layers are offloaded to a GPU that pool is the accelerator's memory, not
// system RAM. On a UMA APU the two are not merely different, they overlap: the
// GPU pool is carved FROM system RAM (62.5 GiB of 125 GiB on Strix Halo), so
// budgeting against total system memory counts the same bytes twice and
// recommends a context that cannot fit. On a discrete GPU the error is larger
// still -- a 24 GB card in a 128 GB host would be budgeted five times its real
// capacity.
//
// With no GPU detected the model runs on the CPU and system RAM is exactly the
// right budget, which is what this returns.
func memoryBudgetGB(hw *HardwareInfo) float64 {
	if hw == nil {
		return 0
	}
	if len(hw.GPUs) > 0 && hw.GPUs[0].MemoryGB > 0 {
		return hw.GPUs[0].MemoryGB
	}
	return hw.TotalMemoryGB
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
		ctx := int(memoryBudgetGB(hw)/8) * 4096
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
	availableGB := (memoryBudgetGB(hw) - modelSizeGB) * 0.9
	if availableGB < 0 {
		availableGB = 0
	}

	// KV cache bytes per token = layers * kv_heads * (key_dim + value_dim) * 2
	// bytes (FP16 for K and V).
	//
	//   - kv_heads captures Grouped-Query Attention: modern models share K/V
	//     across query-head groups, so the cache scales with the KV-head count,
	//     not the query-head count. Using the full width overestimates the KV
	//     cache by the GQA ratio (often 4-8x) and understates the usable context.
	//   - key_dim/value_dim are the model's explicit per-head dimensions when
	//     present (they can exceed embedding/head_count, e.g. asymmetric or
	//     MLA-style attention); using them avoids UNDER-estimating the cache and
	//     over-recommending context past what fits.
	//
	// When head geometry is unavailable, fall back to the full embedding width
	// for both K and V — a conservative overestimate.
	kvHeads := meta.HeadCount
	if meta.KVHeadCount > 0 {
		kvHeads = meta.KVHeadCount
	}
	headDim := 0
	if meta.HeadCount > 0 {
		headDim = meta.EmbeddingSize / meta.HeadCount
	}
	keyDim := headDim
	if meta.KeyLength > 0 {
		keyDim = meta.KeyLength
	}
	valueDim := headDim
	if meta.ValueLength > 0 {
		valueDim = meta.ValueLength
	}

	var kvBytesPerToken float64
	if kvHeads > 0 && keyDim > 0 && valueDim > 0 {
		kvBytesPerToken = float64(meta.LayerCount) * float64(kvHeads) * float64(keyDim+valueDim) * 2.0
	} else {
		// No usable head geometry: full embedding width for both K and V.
		kvBytesPerToken = 2.0 * float64(meta.LayerCount) * float64(meta.EmbeddingSize) * 2.0
	}

	if kvBytesPerToken <= 0 {
		return defaultContext
	}

	// Convert available GB to bytes.
	availableBytes := availableGB * 1024 * 1024 * 1024

	maxTokens := int(availableBytes / kvBytesPerToken)

	// Cap at the model's trained context length if known.
	if meta.ContextLength > 0 && maxTokens > meta.ContextLength {
		maxTokens = meta.ContextLength
	}

	// Enforce a practical minimum, but never raise past the trained window — a
	// model trained on fewer than minContext tokens must not be recommended more
	// than it was trained for.
	if maxTokens < minContext {
		maxTokens = minContext
		if meta.ContextLength > 0 && maxTokens > meta.ContextLength {
			maxTokens = meta.ContextLength
		}
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

	// Deliberately still keyed on SYSTEM memory, not the GPU budget. These
	// thresholds were tuned against total RAM, and re-basing them on a smaller
	// GPU pool would silently drop a 24 GB discrete card from 2048 to 512 --
	// a throughput regression, not a correctness fix. Batch buffers are modest
	// next to weights and KV cache, so the 2x error that matters for context
	// does not matter here.
	//
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
