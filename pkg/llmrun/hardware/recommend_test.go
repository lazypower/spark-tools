package hardware

import (
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

func TestRecommendConfig_NilHardware(t *testing.T) {
	cfg := RecommendConfig(nil, nil)
	if cfg.Threads != 1 {
		t.Errorf("Threads = %d, want 1", cfg.Threads)
	}
	if cfg.GPULayers != 0 {
		t.Errorf("GPULayers = %d, want 0", cfg.GPULayers)
	}
	if cfg.ContextSize != 2048 {
		t.Errorf("ContextSize = %d, want 2048", cfg.ContextSize)
	}
	if cfg.BatchSize != 512 {
		t.Errorf("BatchSize = %d, want 512", cfg.BatchSize)
	}
}

func TestRecommendConfig_BasicCPU(t *testing.T) {
	hw := &HardwareInfo{
		CPUName:       "Intel Core i7",
		CPUCores:      8,
		TotalMemoryGB: 32,
		FreeMemoryGB:  24,
	}

	cfg := RecommendConfig(hw, nil)

	// Threads = cores - 2
	if cfg.Threads != 6 {
		t.Errorf("Threads = %d, want 6", cfg.Threads)
	}

	// No GPUs -> CPU-only
	if cfg.GPULayers != 0 {
		t.Errorf("GPULayers = %d, want 0", cfg.GPULayers)
	}

	if !cfg.FlashAttention {
		t.Error("FlashAttention should be true")
	}
	if !cfg.MMap {
		t.Error("MMap should be true")
	}
	if cfg.MLock {
		t.Error("MLock should be false for non-DGX hardware")
	}
}

func TestRecommendConfig_WithGPU(t *testing.T) {
	hw := &HardwareInfo{
		CPUName:       "AMD Ryzen 9",
		CPUCores:      16,
		TotalMemoryGB: 64,
		FreeMemoryGB:  48,
		GPUs: []GPUInfo{
			{Index: 0, Name: "NVIDIA GeForce RTX 4090", MemoryGB: 24},
		},
	}

	cfg := RecommendConfig(hw, nil)

	if cfg.GPULayers != -1 {
		t.Errorf("GPULayers = %d, want -1 (all layers offloaded)", cfg.GPULayers)
	}
	if cfg.Threads != 14 {
		t.Errorf("Threads = %d, want 14", cfg.Threads)
	}
}

func TestRecommendConfig_DGXSpark(t *testing.T) {
	hw := &HardwareInfo{
		CPUName:       "NVIDIA Grace",
		CPUCores:      12,
		TotalMemoryGB: 128,
		FreeMemoryGB:  120,
		GPUs: []GPUInfo{
			{Index: 0, Name: "NVIDIA GB10", MemoryGB: 128, Compute: "sm_100"},
		},
		IsDGXSpark: true,
	}

	meta := &gguf.GGUFMetadata{
		Architecture:   "llama",
		ParameterCount: 32_000_000_000,
		ContextLength:  32768,
		QuantType:      "Q4_K_M",
		LayerCount:     64,
		EmbeddingSize:  5120,
	}

	cfg := RecommendConfig(hw, meta)

	// DGX Spark defaults
	if cfg.GPULayers != -1 {
		t.Errorf("GPULayers = %d, want -1", cfg.GPULayers)
	}
	if cfg.NumaStrategy != engine.NumaDistribute {
		t.Errorf("NumaStrategy = %v, want NumaDistribute", cfg.NumaStrategy)
	}
	if !cfg.FlashAttention {
		t.Error("FlashAttention should be true on DGX Spark")
	}
	if !cfg.MLock {
		t.Error("MLock should be true on DGX Spark")
	}
}

func TestRecommendConfig_SmallMachine(t *testing.T) {
	hw := &HardwareInfo{
		CPUName:       "ARM Cortex",
		CPUCores:      2,
		TotalMemoryGB: 4,
		FreeMemoryGB:  2,
	}

	cfg := RecommendConfig(hw, nil)

	// Threads should be at least 1 even with 2 cores.
	if cfg.Threads < 1 {
		t.Errorf("Threads = %d, want >= 1", cfg.Threads)
	}

	// Small memory -> smaller batch size.
	if cfg.BatchSize != 256 {
		t.Errorf("BatchSize = %d, want 256", cfg.BatchSize)
	}
}

func TestEstimateMaxContext(t *testing.T) {
	tests := []struct {
		name     string
		hw       *HardwareInfo
		meta     *gguf.GGUFMetadata
		wantMin  int
		wantMax  int
	}{
		{
			name:    "nil hardware",
			hw:      nil,
			meta:    nil,
			wantMin: 4096,
			wantMax: 4096,
		},
		{
			name: "nil metadata, 32GB RAM",
			hw: &HardwareInfo{
				TotalMemoryGB: 32,
			},
			meta:    nil,
			wantMin: 4096,
			wantMax: 32768,
		},
		{
			name: "nil metadata, 128GB RAM",
			hw: &HardwareInfo{
				TotalMemoryGB: 128,
			},
			meta:    nil,
			wantMin: 32768,
			wantMax: 32768,
		},
		{
			name: "32B model Q4_K_M on 128GB",
			hw: &HardwareInfo{
				TotalMemoryGB: 128,
			},
			meta: &gguf.GGUFMetadata{
				ParameterCount: 32_000_000_000,
				ContextLength:  32768,
				QuantType:      "Q4_K_M",
				LayerCount:     64,
				EmbeddingSize:  5120,
			},
			wantMin: 8192,   // Should be reasonably large
			wantMax: 32768,  // Capped at model's trained length
		},
		{
			name: "7B model Q4_K_M on 16GB",
			hw: &HardwareInfo{
				TotalMemoryGB: 16,
			},
			meta: &gguf.GGUFMetadata{
				ParameterCount: 7_000_000_000,
				ContextLength:  8192,
				QuantType:      "Q4_K_M",
				LayerCount:     32,
				EmbeddingSize:  4096,
			},
			wantMin: 2048,
			wantMax: 8192, // Capped at trained context
		},
		{
			name: "huge model barely fits",
			hw: &HardwareInfo{
				TotalMemoryGB: 24,
			},
			meta: &gguf.GGUFMetadata{
				ParameterCount: 70_000_000_000,
				ContextLength:  8192,
				QuantType:      "Q4_K_M",
				LayerCount:     80,
				EmbeddingSize:  8192,
			},
			wantMin: 512, // Minimum enforced
			wantMax: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateMaxContext(tt.hw, tt.meta)
			if got < tt.wantMin {
				t.Errorf("estimateMaxContext() = %d, want >= %d", got, tt.wantMin)
			}
			if got > tt.wantMax {
				t.Errorf("estimateMaxContext() = %d, want <= %d", got, tt.wantMax)
			}
			t.Logf("estimateMaxContext() = %d (expected range [%d, %d])", got, tt.wantMin, tt.wantMax)
		})
	}
}

func TestRecommendBatchSize(t *testing.T) {
	tests := []struct {
		name    string
		memGB   float64
		want    int
	}{
		{"4GB RAM", 4, 256},
		{"8GB RAM", 8, 256},
		{"16GB RAM", 16, 512},
		{"32GB RAM", 32, 512},
		{"64GB RAM", 64, 2048},
		{"128GB RAM", 128, 2048},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hw := &HardwareInfo{TotalMemoryGB: tt.memGB}
			got := recommendBatchSize(hw, nil)
			if got != tt.want {
				t.Errorf("recommendBatchSize(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestEstimateModelSizeGB(t *testing.T) {
	tests := []struct {
		name     string
		meta     *gguf.GGUFMetadata
		wantMin  float64
		wantMax  float64
	}{
		{
			name: "7B Q4_K_M",
			meta: &gguf.GGUFMetadata{
				ParameterCount: 7_000_000_000,
				QuantType:      "Q4_K_M",
			},
			wantMin: 3.0,
			wantMax: 5.0,
		},
		{
			name: "32B Q4_K_M",
			meta: &gguf.GGUFMetadata{
				ParameterCount: 32_000_000_000,
				QuantType:      "Q4_K_M",
			},
			wantMin: 15.0,
			wantMax: 25.0,
		},
		{
			name: "70B Q4_K_M",
			meta: &gguf.GGUFMetadata{
				ParameterCount: 70_000_000_000,
				QuantType:      "Q4_K_M",
			},
			wantMin: 30.0,
			wantMax: 50.0,
		},
		{
			name: "nil metadata",
			meta: nil,
			wantMin: 0,
			wantMax: 0,
		},
		{
			name: "unknown quant falls back to Q4_K_M",
			meta: &gguf.GGUFMetadata{
				ParameterCount: 7_000_000_000,
				QuantType:      "",
			},
			wantMin: 3.0,
			wantMax: 5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateModelSizeGB(tt.meta)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("estimateModelSizeGB() = %.2f GB, want [%.1f, %.1f]", got, tt.wantMin, tt.wantMax)
			}
			t.Logf("estimateModelSizeGB() = %.2f GB", got)
		})
	}
}

func TestRoundDownContext(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 512},
		{100, 0},    // 100/256*256 = 0
		{512, 512},
		{1000, 768},  // 1000/256*256 = 768
		{4096, 4096},
		{8192, 8192},
		{10000, 9984}, // 10000/256*256 = 9984
		{32768, 32768},
		{40000, 39936}, // 40000/1024*1024 = 39936
		{65536, 65536},
		{131072, 131072},
	}

	for _, tt := range tests {
		got := roundDownContext(tt.input)
		if got != tt.want {
			t.Errorf("roundDownContext(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestApplyDGXSparkDefaults(t *testing.T) {
	cfg := &engine.RunConfig{}
	ApplyDGXSparkDefaults(cfg)

	if cfg.GPULayers != -1 {
		t.Errorf("GPULayers = %d, want -1", cfg.GPULayers)
	}
	if cfg.NumaStrategy != engine.NumaDistribute {
		t.Errorf("NumaStrategy = %v, want NumaDistribute", cfg.NumaStrategy)
	}
	if !cfg.FlashAttention {
		t.Error("FlashAttention should be true")
	}
	if !cfg.MLock {
		t.Error("MLock should be true")
	}

	// Nil config should not panic.
	ApplyDGXSparkDefaults(nil)
}
