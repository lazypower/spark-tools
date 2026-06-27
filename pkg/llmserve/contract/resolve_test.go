package contract

import (
	"slices"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// qwenFacts is a verified Qwen3 MoE NVFP4 artifact with a Qwen tokenizer.
func qwenFacts() serving.ArtifactFacts {
	return serving.ArtifactFacts{
		ModelID:   "Qwen/Qwen3.6-35B-A3B-NVFP4",
		Revision:  "abc123",
		ModelPath: "/models/hf/Qwen3.6-35B-A3B-NVFP4",
		Arch:      "Qwen3MoeForCausalLM",
		Tokenizer: serving.TokenizerQwen,
		Quant:     serving.QuantNVFP4,
	}
}

// hasFlag reports whether flags contains the given flag token.
func hasFlag(flags []string, f string) bool { return slices.Contains(flags, f) }

// flagValue returns the token after the named flag, or "".
func flagValue(flags []string, name string) string {
	for i, f := range flags {
		if f == name && i+1 < len(flags) {
			return flags[i+1]
		}
	}
	return ""
}

func TestResolve_NVFP4_NoQuantFlag(t *testing.T) {
	got, err := Resolve(Request{ServedName: "qwen-36b-fp4"}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasFlag(got.Flags, "--quantization") {
		t.Errorf("NVFP4 is auto-detected and must NOT emit --quantization; flags=%v", got.Flags)
	}
}

func TestResolve_GPTQ_NeedsMoeWna16(t *testing.T) {
	facts := qwenFacts()
	facts.Quant = serving.QuantGPTQ
	facts.ModelPath = "/models/hf/Qwen3.6-35B-A3B-GPTQ-Int4"
	got, err := Resolve(Request{ServedName: "qwen-36b"}, facts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if v := flagValue(got.Flags, "--quantization"); v != "moe_wna16" {
		t.Errorf("GPTQ MoE must emit --quantization moe_wna16, got %q; flags=%v", v, got.Flags)
	}
}

func TestResolve_UnknownQuant_Rejected(t *testing.T) {
	facts := qwenFacts()
	facts.Quant = serving.QuantMethod("awq-future")
	_, err := Resolve(Request{ServedName: "x"}, facts)
	re, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected rejection for unknown quant, got %v", err)
	}
	if re.Rule != "unknown-quant" {
		t.Errorf("expected unknown-quant rule, got %q", re.Rule)
	}
}

func TestResolve_UnknownArch_Rejected(t *testing.T) {
	facts := qwenFacts()
	facts.Arch = "MysteryForCausalLM"
	_, err := Resolve(Request{ServedName: "x"}, facts)
	if re, ok := AsRejection(err); !ok || re.Rule != "unknown-arch" {
		t.Fatalf("expected unknown-arch rejection, got %v", err)
	}
}

func TestResolve_Thinking_EmitsReasoningParser(t *testing.T) {
	got, err := Resolve(Request{
		ServedName:   "qwen-36b-fp4",
		Capabilities: []serving.Capability{serving.Thinking},
	}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if v := flagValue(got.Flags, "--reasoning-parser"); v != "qwen3" {
		t.Errorf("thinking must emit --reasoning-parser qwen3, got %q", v)
	}
	if v := flagValue(got.Flags, "--default-chat-template-kwargs"); !strings.Contains(v, "true") {
		t.Errorf("thinking must enable_thinking=true, got %q", v)
	}
}

func TestResolve_NoThinking_OmitsReasoningParser_KeepsGuidedLive(t *testing.T) {
	// The AGENTS.md root-cause: a reasoning parser silently disables guided
	// decoding. A coder request (guided-decoding, no thinking) must NOT carry a
	// reasoning parser.
	got, err := Resolve(Request{
		ServedName:   "qwen-coder-30b",
		Capabilities: []serving.Capability{serving.GuidedDecoding},
	}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasFlag(got.Flags, "--reasoning-parser") {
		t.Errorf("guided-decoding without thinking must NOT emit a reasoning parser; flags=%v", got.Flags)
	}
	if v := flagValue(got.Flags, "--default-chat-template-kwargs"); !strings.Contains(v, "false") {
		t.Errorf("no thinking must enable_thinking=false, got %q", v)
	}
}

func TestResolve_Reject_ThinkingPlusGuided(t *testing.T) {
	_, err := Resolve(Request{
		ServedName:   "qwen-36b-fp4",
		Capabilities: []serving.Capability{serving.Thinking, serving.GuidedDecoding},
	}, qwenFacts())
	re, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected rejection for thinking+guided, got %v", err)
	}
	if re.Rule != "reasoning-parser-disables-guided-decoding" {
		t.Errorf("wrong rule fired: %q", re.Rule)
	}
}

func TestResolve_ToolCalling_Qwen_EmitsParser(t *testing.T) {
	got, err := Resolve(Request{
		ServedName:   "qwen-coder-30b",
		Capabilities: []serving.Capability{serving.ToolCalling},
	}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !hasFlag(got.Flags, "--enable-auto-tool-choice") {
		t.Errorf("tool-calling must emit --enable-auto-tool-choice; flags=%v", got.Flags)
	}
	if v := flagValue(got.Flags, "--tool-call-parser"); v != "qwen3_coder" {
		t.Errorf("tool-calling must emit --tool-call-parser qwen3_coder, got %q", v)
	}
}

func TestResolve_Reject_ToolCalling_NonQwenTokenizer(t *testing.T) {
	// qwen3_coder 500s on a non-Qwen tokenizer. Same arch, but a generic
	// tokenizer artifact must be rejected.
	facts := qwenFacts()
	facts.Tokenizer = serving.TokenizerGeneric
	_, err := Resolve(Request{
		ServedName:   "x",
		Capabilities: []serving.Capability{serving.ToolCalling},
	}, facts)
	re, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected rejection for tool-calling on non-Qwen tokenizer, got %v", err)
	}
	if re.Rule != "tool-parser-requires-matching-tokenizer" {
		t.Errorf("wrong rule fired: %q", re.Rule)
	}
}

func TestResolve_Reject_MistralTokenizerMode_Vision(t *testing.T) {
	facts := serving.ArtifactFacts{
		ModelID:   "mistralai/Mistral-Small-3-Vision",
		ModelPath: "/models/hf/Mistral3",
		Arch:      "Mistral3ForConditionalGeneration",
		Tokenizer: serving.TokenizerMistral,
		Quant:     serving.QuantNone,
		HasVision: true,
	}
	_, err := Resolve(Request{
		ServedName:   "mistral3",
		Capabilities: []serving.Capability{serving.Vision},
	}, facts)
	re, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected rejection for mistral tokenizer-mode + vision, got %v", err)
	}
	if re.Rule != "mistral-tokenizer-mode-breaks-vision" {
		t.Errorf("wrong rule fired: %q", re.Rule)
	}
}

func TestResolve_Nemotron_TrustRemoteCode(t *testing.T) {
	facts := serving.ArtifactFacts{
		ModelID:         "nvidia/Nemotron-3-Nano-30B-A3B-NVFP4",
		ModelPath:       "/models/hf/Nemotron-3-Nano-30B-A3B-NVFP4",
		Arch:            "NemotronHForCausalLM",
		Tokenizer:       serving.TokenizerGeneric,
		Quant:           serving.QuantNVFP4,
		NeedsRemoteCode: true,
	}
	got, err := Resolve(Request{ServedName: "nemotron-nano"}, facts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !hasFlag(got.Flags, "--trust-remote-code") {
		t.Errorf("Nemotron-H must emit --trust-remote-code; flags=%v", got.Flags)
	}
}

func TestResolve_Reject_UnsupportedCapability(t *testing.T) {
	// GLM4 MoE does not claim thinking.
	facts := serving.ArtifactFacts{
		ModelID:   "zai/GLM-4.7-Flash-NVFP4",
		ModelPath: "/models/hf/GLM-4.7-Flash-NVFP4",
		Arch:      "Glm4MoeForCausalLM",
		Tokenizer: serving.TokenizerGeneric,
		Quant:     serving.QuantNVFP4,
	}
	_, err := Resolve(Request{
		ServedName:   "glm-47-flash",
		Capabilities: []serving.Capability{serving.Thinking},
	}, facts)
	if re, ok := AsRejection(err); !ok || re.Rule != "unsupported-capability" {
		t.Fatalf("expected unsupported-capability rejection, got %v", err)
	}
}

func TestResolve_ContractKey_ModeIsOrderIndependent(t *testing.T) {
	a, err := Resolve(Request{
		ServedName:   "qwen",
		Capabilities: []serving.Capability{serving.Vision, serving.ToolCalling},
	}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve a: %v", err)
	}
	b, err := Resolve(Request{
		ServedName:   "qwen",
		Capabilities: []serving.Capability{serving.ToolCalling, serving.Vision},
	}, qwenFacts())
	if err != nil {
		t.Fatalf("resolve b: %v", err)
	}
	if a.Key.Mode != b.Key.Mode {
		t.Errorf("mode label must be order-independent: %q vs %q", a.Key.Mode, b.Key.Mode)
	}
}

func TestResolve_RequiresServedNameAndPath(t *testing.T) {
	if _, err := Resolve(Request{}, qwenFacts()); err == nil {
		t.Error("empty served name must be rejected")
	}
	facts := qwenFacts()
	facts.ModelPath = ""
	if _, err := Resolve(Request{ServedName: "x"}, facts); err == nil {
		t.Error("missing model path must be rejected")
	}
}
