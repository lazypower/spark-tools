package hardware

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/gguf"
)

func TestMemoryBudgetGB(t *testing.T) {
	cases := []struct {
		name string
		hw   *HardwareInfo
		want float64
	}{
		{
			// Strix Halo: the GPU pool is carved from system RAM, so budgeting
			// against the 125 GB counts the same bytes twice.
			name: "UMA APU uses the GPU pool, not system RAM",
			hw: &HardwareInfo{
				TotalMemoryGB: 125.1,
				GPUs:          []GPUInfo{{Vendor: VendorAMD, MemoryGB: 62.5, Compute: "gfx1151"}},
			},
			want: 62.5,
		},
		{
			name: "discrete GPU is bounded by card memory",
			hw: &HardwareInfo{
				TotalMemoryGB: 128,
				GPUs:          []GPUInfo{{Vendor: VendorNVIDIA, MemoryGB: 24}},
			},
			want: 24,
		},
		{
			// No accelerator: the model runs on the CPU and system RAM is
			// exactly the right budget.
			name: "no GPU falls back to system RAM",
			hw:   &HardwareInfo{TotalMemoryGB: 64},
			want: 64,
		},
		{
			name: "GPU reporting no memory falls back to system RAM",
			hw: &HardwareInfo{
				TotalMemoryGB: 64,
				GPUs:          []GPUInfo{{Vendor: VendorAMD, MemoryGB: 0}},
			},
			want: 64,
		},
		{name: "nil hardware", hw: nil, want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := memoryBudgetGB(c.hw); got != c.want {
				t.Errorf("memoryBudgetGB = %v, want %v", got, c.want)
			}
		})
	}
}

// The regression this guards: with layers offloaded, a context sized against
// system RAM does not fit in the accelerator. A smaller GPU must yield a
// smaller recommendation than the same box with no GPU at all.
//
// The model is chosen so the memory budget is what BINDS: a wide KV cache (no
// GQA) and a trained window far past what either budget affords, so neither
// side is clamped at the trained length and the two estimates differ purely by
// available memory.
func TestEstimateMaxContext_GPUPoolBoundsTheRecommendation(t *testing.T) {
	meta := &gguf.GGUFMetadata{
		ParameterCount: 13_000_000_000,
		ContextLength:  262144,
		QuantType:      "Q4_K_M",
		LayerCount:     40,
		EmbeddingSize:  5120,
		HeadCount:      40,
		KVHeadCount:    40, // no GQA: full-width KV, so the cache actually bites
	}

	cpuOnly := &HardwareInfo{TotalMemoryGB: 128}
	withGPU := &HardwareInfo{
		TotalMemoryGB: 128,
		GPUs:          []GPUInfo{{Vendor: VendorNVIDIA, MemoryGB: 24}},
	}

	big := estimateMaxContext(cpuOnly, meta)
	small := estimateMaxContext(withGPU, meta)

	if small >= big {
		t.Errorf("a 24 GB GPU in a 128 GB host must bound the context (%d) below the CPU-only estimate (%d)", small, big)
	}
	if small <= 0 {
		t.Errorf("context estimate must stay positive, got %d", small)
	}
	if big >= meta.ContextLength {
		t.Errorf("test is not exercising the memory budget: CPU-only estimate %d hit the trained cap %d", big, meta.ContextLength)
	}
}

// Same shape without model metadata, which takes the other branch.
func TestEstimateMaxContext_NoMetadata_UsesGPUPool(t *testing.T) {
	strix := &HardwareInfo{
		TotalMemoryGB: 125.1,
		GPUs:          []GPUInfo{{Vendor: VendorAMD, MemoryGB: 62.5, Compute: "gfx1151"}},
	}
	// 62.5/8 = 7 -> 7*4096 = 28672, below the 32768 the 125 GB figure produced.
	if got := estimateMaxContext(strix, nil); got != 28672 {
		t.Errorf("context = %d, want 28672 (derived from the 62.5 GiB pool, not 125 GB of RAM)", got)
	}
}

// Batch size deliberately keeps the system-RAM basis: re-basing it on a smaller
// GPU pool would drop a 24 GB card from 2048 to 512, a throughput regression
// rather than a correctness fix.
func TestRecommendBatchSize_UnaffectedByGPUPool(t *testing.T) {
	hw := &HardwareInfo{
		TotalMemoryGB: 128,
		GPUs:          []GPUInfo{{Vendor: VendorNVIDIA, MemoryGB: 24}},
	}
	if got := recommendBatchSize(hw, nil); got != 2048 {
		t.Errorf("batch = %d, want 2048 (system-RAM basis retained)", got)
	}
}
