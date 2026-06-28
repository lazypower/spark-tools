package gguf

import (
	"fmt"
	"os"
	"strconv"
)

// FitStatus indicates whether a model will fit in available memory.
type FitStatus int

const (
	FitYes     FitStatus = iota // Model fits comfortably
	FitTight                    // Model fits but tight (>80% of available)
	FitNo                       // Model won't fit
	FitUnknown                  // Can't determine (missing metadata)
)

// FitResult describes whether and how a model fits in available memory.
type FitResult struct {
	Status         FitStatus
	Label          string  // "✓ Fits", "⚠ Tight", "✗ Won't fit", "? Unknown"
	EstimatedGB    float64 // Estimated total runtime memory in GB
	AvailableGB    float64 // Available memory in GB
	ModelWeightsGB float64 // Model file size in GB
	KVCacheGB      float64 // Estimated KV cache at default context
}

// EstimateFit estimates whether a model file will fit in available memory.
// availableGB is total available memory; if 0, it's read from HFETCH_VRAM
// env var or defaults to 0 (unknown).
// contextLength is the target context size; if 0, uses the model's trained length.
func EstimateFit(fileSizeBytes int64, meta *GGUFMetadata, availableGB float64) FitResult {
	if availableGB <= 0 {
		availableGB = detectVRAM()
	}

	result := FitResult{
		AvailableGB: availableGB,
	}

	if availableGB <= 0 {
		result.Status = FitUnknown
		result.Label = "? Unknown"
		return result
	}

	// Model weights: file size is the dominant factor.
	result.ModelWeightsGB = float64(fileSizeBytes) / (1024 * 1024 * 1024)

	// KV cache estimate.
	// KV cache size ≈ 2 × layers × heads × head_dim × context × 2 bytes (FP16)
	// Simplified: use parameter count and context length for a rough estimate.
	// Rule of thumb: KV cache ≈ (2 × n_layers × d_model × context × 2) / 1e9 GB
	if meta != nil && meta.LayerCount > 0 && meta.EmbeddingSize > 0 {
		ctx := meta.ContextLength
		if ctx <= 0 {
			ctx = 4096 // conservative default
		}
		// KV cache: 2 (K+V) × layers × embedding × context × 2 bytes (FP16)
		kvBytes := float64(2) * float64(meta.LayerCount) * float64(meta.EmbeddingSize) * float64(ctx) * 2
		result.KVCacheGB = kvBytes / (1024 * 1024 * 1024)
	}

	result.EstimatedGB = result.ModelWeightsGB + result.KVCacheGB

	// Add ~10% overhead for runtime buffers, scratch space.
	totalNeeded := result.EstimatedGB * 1.1

	ratio := totalNeeded / availableGB
	switch {
	case ratio <= 0.80:
		result.Status = FitYes
		result.Label = "✓ Fits"
	case ratio <= 1.0:
		result.Status = FitTight
		result.Label = "⚠ Tight"
	default:
		result.Status = FitNo
		result.Label = "✗ Won't fit"
	}

	return result
}

// FitLabel returns a short label suitable for terminal display.
func (f FitResult) FitLabel() string {
	if f.Status == FitUnknown {
		return ""
	}
	return fmt.Sprintf("%s (%.0f/%.0f GB)", f.Label, f.EstimatedGB, f.AvailableGB)
}

// detectVRAM reads available memory from HFETCH_VRAM env var.
func detectVRAM() float64 {
	v := os.Getenv("HFETCH_VRAM")
	if v == "" {
		return 0
	}
	gb, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return gb
}
