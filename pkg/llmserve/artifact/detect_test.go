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

func TestDetectFacts_NoQuantMetadata_IsNone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"architectures":["Qwen3MoeForCausalLM"]}`)
	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if facts.Quant != serving.QuantNone {
		t.Errorf("absent quant metadata must be QuantNone, got %q", facts.Quant)
	}
}

func TestDetectFacts_UnknownQuant_NotDowngraded(t *testing.T) {
	// codex P1: present-but-unrecognized quant metadata must NOT collapse to a
	// known-safe method, or it slips past the resolver's unknown-quant gate.
	cases := []struct {
		name        string
		quantConfig string // contents of config.json quantization_config or sidecar
		viaSidecar  bool
	}{
		{"embedded-awq", `{"architectures":["Qwen3MoeForCausalLM"],"quantization_config":{"quant_method":"awq","bits":4}}`, false},
		{"modelopt-int8", `{"quantization":{"quant_algo":"INT8"}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.viaSidecar {
				writeFile(t, dir, "config.json", `{"architectures":["Qwen3MoeForCausalLM"]}`)
				writeFile(t, dir, "hf_quant_config.json", c.quantConfig)
			} else {
				writeFile(t, dir, "config.json", c.quantConfig)
			}
			facts, err := DetectFacts(dir)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			switch facts.Quant {
			case serving.QuantNone, serving.QuantNVFP4, serving.QuantFP8,
				serving.QuantGPTQ, serving.QuantCompressedTensors:
				t.Errorf("unrecognized quant must surface as an unknown method, got known %q", facts.Quant)
			}
			if facts.Quant == "" {
				t.Error("unrecognized quant must be a non-empty unknown label, not empty (QuantNone)")
			}
		})
	}
}
