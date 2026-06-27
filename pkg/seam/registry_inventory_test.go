package seam

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

// Seam: hfetch registry (writer) <-> llm-tidy inventory (reader).
//
// CONTRACT: a serve-ready safetensors model written to the hfetch registry by
// `hfetch pull --profile vllm` (many non-GGUF files: shards, config, tokenizer)
// must NOT be surfaced by llm-tidy's GGUF inventory as GGUF rows.
//
// STATUS: RED. inventory.GGUFList lists every completed registry file as
// BackendGGUF, so a vLLM pull leaks in as bogus GGUF entries (one row per
// shard/config/tokenizer). This test documents the broken contract; it turns
// green when llm-tidy stops treating non-.gguf registry files as GGUF.
func TestSeam_RegistryVLLMEntry_NotSurfacedAsGGUF(t *testing.T) {
	dir := t.TempDir()
	reg := registry.New(dir)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}

	// Exactly what `hfetch pull --profile vllm` registers today.
	const repo = "nvidia/Qwen3.6-35B-A3B-NVFP4"
	for _, f := range []string{
		"model-00001-of-00002.safetensors",
		"model-00002-of-00002.safetensors",
		"model.safetensors.index.json",
		"config.json",
		"tokenizer.json",
	} {
		reg.AddFile(repo, registry.LocalFile{Filename: f, Complete: true})
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	models, err := inventory.GGUFList(reg)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range models {
		if !strings.HasSuffix(m.Filename, ".gguf") {
			t.Errorf("SEAM CONTRACT BROKEN (hfetch -> llm-tidy): non-GGUF file %q from a vLLM pull surfaced as backend %v",
				m.Filename, m.Backend)
		}
	}
}
