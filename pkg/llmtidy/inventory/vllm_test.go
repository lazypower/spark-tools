package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// registerModel writes the given files into a real dir and records them in the
// registry with their LocalPath, returning the model dir.
func registerModel(t *testing.T, reg *registry.Registry, repo string, files ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		reg.AddFile(repo, registry.LocalFile{Filename: f, Complete: true, LocalPath: p, Size: 1})
	}
	if err := reg.Save(); err != nil { // VLLMList/GGUFList reload from disk
		t.Fatal(err)
	}
	return dir
}

func TestVLLMList_SurfacesSafetensorsModel_AtDirGranularity(t *testing.T) {
	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	dir := registerModel(t, reg, "org/QwenVLLM",
		"model-00001-of-00002.safetensors", "model-00002-of-00002.safetensors", "config.json", "tokenizer.json")
	// A gguf model in the same registry must NOT appear in the vLLM list.
	registerModel(t, reg, "org/LlamaGGUF", "llama.Q4_K_M.gguf")

	models, err := VLLMList(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected exactly one vLLM model (dir granularity), got %d: %+v", len(models), models)
	}
	m := models[0]
	if m.Backend != BackendVLLM || m.Repo != "org/QwenVLLM" {
		t.Errorf("wrong model surfaced: %+v", m)
	}
	if m.Path != dir {
		t.Errorf("Path must be the model dir (what llm-serve protects), got %q want %q", m.Path, dir)
	}
}

func TestVLLMList_ExcludesGGUF_AndGGUFExcludesVLLM(t *testing.T) {
	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	registerModel(t, reg, "org/Vllm", "model.safetensors", "config.json")
	registerModel(t, reg, "org/Gguf", "m.Q4_K_M.gguf")

	vllm, _ := VLLMList(reg)
	for _, m := range vllm {
		if m.Repo == "org/Gguf" {
			t.Error("VLLMList must not surface a gguf model")
		}
	}
	gguf, _ := GGUFList(reg)
	for _, m := range gguf {
		if m.Repo == "org/Vllm" {
			t.Error("GGUFList must not surface a vLLM (safetensors) model")
		}
	}
}

func TestVLLMDelete_RemovesTheModelDir(t *testing.T) {
	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	dir := registerModel(t, reg, "org/Doomed", "model.safetensors", "config.json")
	models, _ := VLLMList(reg)
	if len(models) != 1 {
		t.Fatalf("setup: want 1 model, got %d", len(models))
	}
	if err := VLLMDelete(reg, models[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("model dir must be gone after delete, stat err=%v", err)
	}
	if reg.Get("org/Doomed") != nil {
		t.Error("registry entry must be removed")
	}
}
