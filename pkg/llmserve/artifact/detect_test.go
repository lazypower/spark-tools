package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectFacts_QwenNVFP4(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"architectures":["Qwen3MoeForCausalLM"],"model_type":"qwen3_moe"}`)
	writeFile(t, dir, "hf_quant_config.json", `{"quantization":{"quant_algo":"NVFP4"}}`)
	writeFile(t, dir, "tokenizer_config.json", `{"tokenizer_class":"Qwen2Tokenizer"}`)

	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if facts.Arch != "Qwen3MoeForCausalLM" {
		t.Errorf("arch = %q", facts.Arch)
	}
	if facts.Tokenizer != serving.TokenizerQwen {
		t.Errorf("tokenizer = %q, want qwen", facts.Tokenizer)
	}
	if facts.Quant != serving.QuantNVFP4 {
		t.Errorf("quant = %q, want nvfp4", facts.Quant)
	}
	if facts.HasVision {
		t.Error("no processor present, HasVision must be false")
	}
}

func TestDetectFacts_GPTQ(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"architectures":["Qwen3MoeForCausalLM"]}`)
	writeFile(t, dir, "quantize_config.json", `{"bits":4}`)
	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if facts.Quant != serving.QuantGPTQ {
		t.Errorf("quant = %q, want gptq", facts.Quant)
	}
}

func TestDetectFacts_MistralVision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"architectures":["Mistral3ForConditionalGeneration"]}`)
	writeFile(t, dir, "tekken.json", `{}`)
	writeFile(t, dir, "processor_config.json", `{}`)
	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if facts.Tokenizer != serving.TokenizerMistral {
		t.Errorf("tokenizer = %q, want mistral", facts.Tokenizer)
	}
	if !facts.HasVision {
		t.Error("processor_config.json present, HasVision must be true")
	}
}

func TestDetectFacts_NemotronRemoteCode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"architectures":["NemotronHForCausalLM"],"auto_map":{"AutoModel":"modeling_nemotron.NemotronHForCausalLM"}}`)
	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !facts.NeedsRemoteCode {
		t.Error("auto_map present, NeedsRemoteCode must be true")
	}
}

func TestDetectFacts_NoConfig_Errors(t *testing.T) {
	if _, err := DetectFacts(t.TempDir()); err == nil {
		t.Error("a directory with no config.json must error")
	}
}
