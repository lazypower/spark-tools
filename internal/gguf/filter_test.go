package gguf

import "testing"

func TestParseQuantFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf", "Q4_K_M"},
		{"model-Q8_0.gguf", "Q8_0"},
		{"model-IQ4_XS.gguf", "IQ4_XS"},
		{"model-Q5_K_S.gguf", "Q5_K_S"},
		{"model-F16.gguf", "F16"},
		{"model-BF16.gguf", "BF16"},
		{"README.md", ""},
		{"model.gguf", ""},
		{"some-model-Q6_K.GGUF", "Q6_K"},
	}

	for _, tt := range tests {
		got := ParseQuantFromFilename(tt.filename)
		if got != tt.want {
			t.Errorf("ParseQuantFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestIsGGUF(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"model.gguf", true},
		{"model.GGUF", true},
		{"README.md", false},
		{"config.json", false},
	}

	for _, tt := range tests {
		got := IsGGUF(tt.filename)
		if got != tt.want {
			t.Errorf("IsGGUF(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestFilterGGUF(t *testing.T) {
	files := []FileInfo{
		{Filename: "model-Q4_K_M.gguf", Quantization: "Q4_K_M"},
		{Filename: "README.md"},
		{Filename: "model-Q8_0.gguf", Quantization: "Q8_0"},
		{Filename: "config.json"},
	}

	result := FilterGGUF(files)
	if len(result) != 2 {
		t.Errorf("expected 2 GGUF files, got %d", len(result))
	}
}

func TestFilterByQuant(t *testing.T) {
	files := []FileInfo{
		{Filename: "model-Q4_K_M.gguf", Quantization: "Q4_K_M"},
		{Filename: "model-Q8_0.gguf", Quantization: "Q8_0"},
		{Filename: "model-Q4_K_S.gguf", Quantization: "Q4_K_S"},
	}

	result := FilterByQuant(files, "q4_k_m") // case-insensitive
	if len(result) != 1 {
		t.Errorf("expected 1 file, got %d", len(result))
	}
	if result[0].Quantization != "Q4_K_M" {
		t.Errorf("expected Q4_K_M, got %q", result[0].Quantization)
	}
}

func TestSortBySize(t *testing.T) {
	files := []FileInfo{
		{Filename: "big.gguf", Size: 30_000_000_000},
		{Filename: "small.gguf", Size: 10_000_000_000},
		{Filename: "medium.gguf", Size: 20_000_000_000},
	}
	SortBySize(files)
	if files[0].Filename != "small.gguf" || files[2].Filename != "big.gguf" {
		t.Errorf("unexpected sort order: %v", files)
	}
}

func TestSortByQuality(t *testing.T) {
	files := []FileInfo{
		{Filename: "low.gguf", BitsPerWeight: 3.44},
		{Filename: "high.gguf", BitsPerWeight: 8.5},
		{Filename: "med.gguf", BitsPerWeight: 4.85},
	}
	SortByQuality(files)
	// Should be sorted descending by bits-per-weight (highest quality first).
	if files[0].Filename != "high.gguf" {
		t.Errorf("expected high.gguf first, got %s", files[0].Filename)
	}
	if files[1].Filename != "med.gguf" {
		t.Errorf("expected med.gguf second, got %s", files[1].Filename)
	}
	if files[2].Filename != "low.gguf" {
		t.Errorf("expected low.gguf last, got %s", files[2].Filename)
	}
}

func TestSortByQuality_SingleElement(t *testing.T) {
	files := []FileInfo{
		{Filename: "only.gguf", BitsPerWeight: 4.85},
	}
	SortByQuality(files)
	if files[0].Filename != "only.gguf" {
		t.Errorf("unexpected result: %v", files)
	}
}

func TestSortByQuality_Empty(t *testing.T) {
	var files []FileInfo
	SortByQuality(files) // should not panic
}

func TestQuantQualityLabel(t *testing.T) {
	tests := []struct {
		quant string
		want  string
	}{
		{"Q4_K_M", "Best balance of quality/size"},
		{"Q5_K_M", "Higher quality"},
		{"Q5_K_S", "Higher quality, smaller"},
		{"Q6_K", "Near-lossless"},
		{"Q8_0", "Highest quality"},
		{"Q4_K_S", "Good quality, compact"},
		{"Q3_K_L", "Lower quality, small"},
		{"Q3_K_M", "Low quality, very small"},
		{"Q3_K_S", "Low quality, very small"},
		{"Q2_K", "Lowest quality"},
		{"IQ4_XS", "Smallest, lower quality"},
		{"IQ4_NL", "Smallest, lower quality"},
		{"IQ3_XXS", "Very small, low quality"},
		{"IQ3_S", "Very small, low quality"},
		{"IQ2_XXS", "Extremely small"},
		{"IQ2_XS", "Extremely small"},
		{"IQ2_S", "Extremely small"},
		{"IQ1_S", "Minimum viable quality"},
		{"IQ1_M", "Minimum viable quality"},
		{"F16", "Full precision (large)"},
		{"BF16", "Full precision (large)"},
		{"F32", "Full precision (very large)"},
		{"UNKNOWN_QUANT", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.quant, func(t *testing.T) {
			got := QuantQualityLabel(tt.quant)
			if got != tt.want {
				t.Errorf("QuantQualityLabel(%q) = %q, want %q", tt.quant, got, tt.want)
			}
		})
	}
}
