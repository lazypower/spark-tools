package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanLocalModels(t *testing.T) {
	dataDir := t.TempDir()
	modelsDir := filepath.Join(dataDir, "models")

	// hfetch uses "org--repo" flat dirs; scanLocalModels must map "--" back to "/".
	repoDir := filepath.Join(modelsDir, "TheBloke--Llama-2-7B-GGUF")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "llama-2-7b-Q4_K_M.gguf"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-gguf file must be ignored.
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := scanLocalModels(dataDir)
	if err != nil {
		t.Fatalf("scanLocalModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 gguf model (README ignored), got %d: %+v", len(models), models)
	}
	m := models[0]
	if m.repo != "TheBloke/Llama-2-7B-GGUF" {
		t.Errorf("repo must map -- to /, got %q", m.repo)
	}
	if m.quant != "Q4_K_M" {
		t.Errorf("quant must be parsed from filename, got %q", m.quant)
	}
	if m.size != 1 {
		t.Errorf("size must come from the file, got %d", m.size)
	}
}

func TestScanLocalModels_MissingDirIsEmpty(t *testing.T) {
	// No models dir → empty result, not an error (fresh install).
	models, err := scanLocalModels(t.TempDir())
	if err != nil {
		t.Fatalf("missing models dir must not error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected no models, got %+v", models)
	}
}

func TestFormatSize_NoKBTier(t *testing.T) {
	// This local formatSize deliberately has no KB tier (unlike internal/progress):
	// sub-MB sizes render as bytes.
	cases := map[int64]string{
		512:                    "512 B",
		1500:                   "1500 B", // < 1MB → bytes, NOT "1.5 KB"
		2 * 1024 * 1024:        "2.0 MB",
		3 * 1024 * 1024 * 1024: "3.0 GB",
	}
	for in, want := range cases {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}
