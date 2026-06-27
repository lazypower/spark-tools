package seam

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/llmserve/artifact"
	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
	"github.com/lazypower/spark-tools/pkg/llmserve/fingerprint"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// Seam: hfetch's completeness gate (pkg/hfetch/fileset) <-> llm-serve's emit
// refusal (pkg/llmserve/artifact.Verify).
//
// CONTRACT: llm-serve must NOT emit a launch for an artifact hfetch would not
// certify serve-ready. The gate is hfetch's single authority (design §5);
// llm-serve delegates to it rather than reimplementing a parallel check. If a
// shard is missing, the gate hard-fails and llm-serve.Verify must refuse — the
// same silent-partial-model class that bit the hand-rolled run.sh.
//
// STATUS: GREEN — guards the delegation. If llm-serve ever grew its own gate (or
// stopped consulting hfetch's), the contract would break here.

func lfsShard(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return api.ModelFile{Type: "file", Filename: name, LFS: &api.LFS{OID: hex.EncodeToString(sum[:]), Size: int64(len(content))}}
}

func plainFile(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return api.ModelFile{Type: "file", Filename: name, Size: int64(len(content))}
}

// completeArtifact lays down a minimal serve-ready Qwen NVFP4 model: one shard,
// config, and a tokenizer — and returns the matching repo tree.
func completeArtifact(t *testing.T) (dir string, repo []api.ModelFile) {
	dir = t.TempDir()
	repo = []api.ModelFile{
		lfsShard(t, dir, "model.safetensors", "WEIGHTS"),
		plainFile(t, dir, "config.json", `{"architectures":["Qwen3MoeForCausalLM"],"model_type":"qwen3_moe"}`),
		plainFile(t, dir, "tokenizer_config.json", `{"tokenizer_class":"Qwen2Tokenizer","chat_template":"x"}`),
		plainFile(t, dir, "tokenizer.json", `{}`),
		plainFile(t, dir, "generation_config.json", `{}`),
		plainFile(t, dir, "hf_quant_config.json", `{"quantization":{"quant_algo":"NVFP4"}}`),
	}
	return dir, repo
}

func TestSeam_ServeRefusesIncompleteArtifact(t *testing.T) {
	dir, repo := completeArtifact(t)

	// Drop the weight shard from disk: the repo tree still lists it (it should be
	// there), but the local pull is missing it — the silent-partial-model case.
	if err := os.Remove(filepath.Join(dir, "model.safetensors")); err != nil {
		t.Fatal(err)
	}

	_, err := artifact.Verify(repo, dir)
	if err == nil {
		t.Fatal("SEAM CONTRACT BROKEN: llm-serve emitted facts for an artifact missing a weight shard; it must delegate to hfetch's gate and refuse")
	}
	if !strings.Contains(err.Error(), "serve-ready") {
		t.Errorf("refusal should name the completeness failure, got: %v", err)
	}
}

func TestSeam_ServeAcceptsCompleteArtifact_AndResolves(t *testing.T) {
	dir, repo := completeArtifact(t)

	facts, err := artifact.Verify(repo, dir)
	if err != nil {
		t.Fatalf("a complete artifact must pass the gate and yield facts, got: %v", err)
	}
	if facts.Arch != "Qwen3MoeForCausalLM" || facts.Quant != serving.QuantNVFP4 || facts.Tokenizer != serving.TokenizerQwen {
		t.Fatalf("detected facts wrong: %+v", facts)
	}

	// And those verified facts resolve into a validated launch contract.
	got, err := contract.Resolve(contract.Request{
		ServedName: "qwen-36b-fp4",
		Target:     fingerprint.Fingerprint{Engine: "vllm/vllm-openai@v0.23.0", Accelerator: "nvidia:gb10:sm121"},
	}, facts)
	if err != nil {
		t.Fatalf("resolve verified artifact: %v", err)
	}
	if got.Key.Arch != "Qwen3MoeForCausalLM" {
		t.Errorf("contract key arch = %q", got.Key.Arch)
	}
}
