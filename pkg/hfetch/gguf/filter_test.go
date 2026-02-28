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
