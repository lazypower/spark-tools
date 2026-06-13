package inventory

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

func seedRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r := registry.New(dir)
	if err := r.Load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}

	now := time.Now().UTC()
	r.AddFile("unsloth/Qwen3.5-122B-A10B-GGUF", registry.LocalFile{
		Filename: "model-Q4_K_M.gguf", Size: 46_000_000_000, Quantization: "Q4_K_M",
		LocalPath: filepath.Join(dir, "models", "unsloth--Qwen3.5", "model-Q4_K_M.gguf"),
		Complete:  true, DownloadedAt: now,
	})
	r.AddFile("unsloth/Qwen3.5-122B-A10B-GGUF", registry.LocalFile{
		Filename: "model-Q5_K_M.gguf", Size: 50_000_000_000, Quantization: "Q5_K_M",
		LocalPath: filepath.Join(dir, "models", "unsloth--Qwen3.5", "model-Q5_K_M.gguf"),
		Complete:  true, DownloadedAt: now,
	})
	// Incomplete file: should be filtered out.
	r.AddFile("mradermacher/Venus", registry.LocalFile{
		Filename: "model-IQ4_XS.gguf", Size: 40_000_000_000,
		LocalPath: filepath.Join(dir, "models", "mradermacher--Venus", "model-IQ4_XS.gguf"),
		Complete:  false, DownloadedAt: now,
	})
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGGUFListEmitsOneRowPerCompleteFile(t *testing.T) {
	dataDir := seedRegistry(t)
	models, err := GGUFList(registry.New(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2 (incomplete should be skipped)", len(models))
	}
	if models[0].Backend != BackendGGUF {
		t.Errorf("backend = %v", models[0].Backend)
	}
	seen := map[string]bool{}
	for _, m := range models {
		seen[m.Quant] = true
		if m.Repo != "unsloth/Qwen3.5-122B-A10B-GGUF" {
			t.Errorf("Repo = %q", m.Repo)
		}
		if m.Name == "" {
			t.Errorf("Name empty")
		}
	}
	if !seen["Q4_K_M"] || !seen["Q5_K_M"] {
		t.Errorf("expected both quants present: %v", seen)
	}
}

func TestGGUFListDisplayNameIncludesQuant(t *testing.T) {
	dir := t.TempDir()
	r := registry.New(dir)
	_ = r.Load()
	r.AddFile("Org/Repo", registry.LocalFile{
		Filename: "m.gguf", Size: 1, Quantization: "Q4_K_M",
		LocalPath: filepath.Join(dir, "m.gguf"), Complete: true,
	})
	r.AddFile("Org/Other", registry.LocalFile{
		Filename: "m.gguf", Size: 1,
		LocalPath: filepath.Join(dir, "m2.gguf"), Complete: true,
	})
	_ = r.Save()

	models, err := GGUFList(registry.New(dir))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, m := range models {
		names[m.Name] = true
	}
	if !names["Org/Repo Q4_K_M"] {
		t.Errorf("missing \"Org/Repo Q4_K_M\": %v", names)
	}
	if !names["Org/Other"] {
		t.Errorf("missing bare \"Org/Other\": %v", names)
	}
}

func TestGGUFDeleteRemovesEntry(t *testing.T) {
	dataDir := seedRegistry(t)

	r := registry.New(dataDir)
	models, err := GGUFList(r)
	if err != nil {
		t.Fatal(err)
	}
	var target InstalledModel
	for _, m := range models {
		if m.Quant == "Q4_K_M" {
			target = m
			break
		}
	}
	if target.Filename == "" {
		t.Fatal("test setup: did not find Q4_K_M")
	}
	if err := GGUFDelete(r, target); err != nil {
		t.Fatal(err)
	}

	remaining, err := GGUFList(registry.New(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range remaining {
		if m.Quant == "Q4_K_M" {
			t.Error("Q4_K_M still present after delete")
		}
	}
}
