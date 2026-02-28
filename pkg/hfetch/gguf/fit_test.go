package gguf

import "testing"

func TestEstimateFit_Fits(t *testing.T) {
	meta := &GGUFMetadata{
		LayerCount:    32,
		EmbeddingSize: 4096,
		ContextLength: 4096,
	}
	// ~20 GB model on 128 GB system.
	result := EstimateFit(20_000_000_000, meta, 128)
	if result.Status != FitYes {
		t.Errorf("expected FitYes, got %v (%s)", result.Status, result.Label)
	}
	if result.ModelWeightsGB < 18 || result.ModelWeightsGB > 20 {
		t.Errorf("unexpected weights: %.1f GB", result.ModelWeightsGB)
	}
}

func TestEstimateFit_Tight(t *testing.T) {
	meta := &GGUFMetadata{
		LayerCount:    32,
		EmbeddingSize: 4096,
		ContextLength: 4096,
	}
	// ~20 GB model on 26 GB system — fits but tight (>80%).
	result := EstimateFit(20_000_000_000, meta, 26)
	if result.Status != FitTight {
		t.Errorf("expected FitTight, got %v (%s, est: %.1f GB, avail: %.1f GB)", result.Status, result.Label, result.EstimatedGB, result.AvailableGB)
	}
}

func TestEstimateFit_WontFit(t *testing.T) {
	meta := &GGUFMetadata{
		LayerCount:    80,
		EmbeddingSize: 8192,
		ContextLength: 32768,
	}
	// 70 GB model on 16 GB system.
	result := EstimateFit(70_000_000_000, meta, 16)
	if result.Status != FitNo {
		t.Errorf("expected FitNo, got %v (%s)", result.Status, result.Label)
	}
}

func TestEstimateFit_Unknown(t *testing.T) {
	result := EstimateFit(20_000_000_000, nil, 0)
	if result.Status != FitUnknown {
		t.Errorf("expected FitUnknown, got %v", result.Status)
	}
}

func TestEstimateFit_EnvVar(t *testing.T) {
	t.Setenv("HFETCH_VRAM", "128")
	meta := &GGUFMetadata{
		LayerCount:    32,
		EmbeddingSize: 4096,
		ContextLength: 4096,
	}
	result := EstimateFit(20_000_000_000, meta, 0) // 0 triggers env var read
	if result.Status != FitYes {
		t.Errorf("expected FitYes from env var, got %v", result.Status)
	}
	if result.AvailableGB != 128 {
		t.Errorf("expected 128 GB from env, got %.0f", result.AvailableGB)
	}
}

func TestFitLabel(t *testing.T) {
	r := FitResult{Status: FitYes, Label: "✓ Fits", EstimatedGB: 20, AvailableGB: 128}
	label := r.FitLabel()
	if label == "" {
		t.Error("expected non-empty label for FitYes")
	}

	r2 := FitResult{Status: FitUnknown}
	if r2.FitLabel() != "" {
		t.Error("expected empty label for FitUnknown")
	}
}
