// Package serving holds the ubiquitous vocabulary of the vLLM serving contract:
// the capabilities a caller can request, the quantization methods detected on an
// artifact, the tokenizer/processor families, the facts read off a verified
// artifact, and the contract key those facts compose into.
//
// It is the leaf of the llm-serve dependency DAG — profiles and contract both
// import it, so the terms have one definition and no package imports back into
// them. The vocabulary is expressed as intent ("guided-decoding on, thinking
// off"), never as backend flags; turning intent into flags is the contract
// package's job (§2 of the design: express intent, not flags).
package serving

import (
	"sort"
	"strings"
)

// Capability is a serving capability a caller asks llm-serve to realize. A
// request names capabilities; the contract package resolves them to vLLM flags
// and rejects incompatible combinations (the negative-compat rules).
type Capability string

const (
	// GuidedDecoding enforces structured output (json_schema/regex/choice/grammar).
	// In vLLM v0.23 this is the request-time structured_outputs field; a reasoning
	// parser silently disables it, which is why it conflicts with Thinking.
	GuidedDecoding Capability = "guided-decoding"
	// Thinking emits chain-of-thought via a reasoning parser (--reasoning-parser).
	Thinking Capability = "thinking"
	// ToolCalling enables auto tool choice (--enable-auto-tool-choice plus a
	// tool-call parser); the parser may require a specific tokenizer family.
	ToolCalling Capability = "tool-calling"
	// Vision enables multimodal image/video input (a multimodal processor).
	Vision Capability = "vision"
)

// AllCapabilities is the v1 capability enum, in canonical order. Adding a new
// serving mode is a one-line extension here plus profile/compat data — never a
// rewrite (the §2 pluggable-seam requirement).
var AllCapabilities = []Capability{GuidedDecoding, Thinking, ToolCalling, Vision}

// QuantMethod is the quantization family detected on an artifact. The mapping
// from method to vLLM flags is hard-won knowledge (NVFP4 needs no flag, GPTQ
// needs moe_wna16) and lives in exactly one place (profiles.QuantFlags).
type QuantMethod string

const (
	// QuantNone is an unquantized (full/half precision) model.
	QuantNone QuantMethod = ""
	// QuantNVFP4 is ModelOpt NVFP4 — auto-detected by vLLM, needs no flag.
	QuantNVFP4 QuantMethod = "nvfp4"
	// QuantGPTQ is GPTQ-Int4 — needs --quantization moe_wna16 for MoE archs.
	QuantGPTQ QuantMethod = "gptq"
	// QuantCompressedTensors is RedHatAI compressed-tensors — the quant lives in
	// config.json (quantization_config), so it needs no launch flag.
	QuantCompressedTensors QuantMethod = "compressed-tensors"
	// QuantFP8 is FP8 weights/KV — auto-detected, needs no flag.
	QuantFP8 QuantMethod = "fp8"
)

// TokenizerFamily identifies the tokenizer/processor an artifact ships, detected
// from the tokenizer files. It is a serving-relevant fact (part of the contract
// key) because parsers and tokenizer-mode flags are family-specific: the
// qwen3_coder tool parser 500s on a non-Qwen tokenizer, and --tokenizer-mode
// mistral crashes on a vision Mistral3.
type TokenizerFamily string

const (
	// TokenizerUnknown means the family could not be determined from the artifact.
	TokenizerUnknown TokenizerFamily = ""
	// TokenizerQwen is the Qwen BPE tokenizer (vocab.json + merges.txt, Qwen tokens).
	TokenizerQwen TokenizerFamily = "qwen"
	// TokenizerMistral is the Mistral/Tekken tokenizer (tekken.json); selects
	// --tokenizer-mode mistral.
	TokenizerMistral TokenizerFamily = "mistral"
	// TokenizerLlama is a SentencePiece/Llama tokenizer.
	TokenizerLlama TokenizerFamily = "llama"
	// TokenizerGeneric is a standard HF tokenizer with no special handling.
	TokenizerGeneric TokenizerFamily = "generic"
)

// ArtifactFacts are the serving-relevant facts read off a verified artifact (a
// model directory that has already passed hfetch's completeness gate). They are
// the *inputs* to contract resolution: the resolver is a pure function of
// (request, facts), and the hfetch boundary is what populates this struct. v1
// reads them; v2's launch probes prove them.
type ArtifactFacts struct {
	// ModelID is the canonical repo/model id (org/model).
	ModelID string
	// Revision is the immutable model revision the artifact was verified at.
	Revision string
	// ModelPath is the path vLLM is pointed at via --model (host or container).
	ModelPath string
	// Arch is the first entry of config.json "architectures", e.g.
	// "Qwen3MoeForCausalLM". The profile registry is keyed on it.
	Arch string
	// Tokenizer is the detected tokenizer/processor family.
	Tokenizer TokenizerFamily
	// Quant is the detected quantization method.
	Quant QuantMethod
	// HasVision is true when a multimodal processor is present (vision/omni model).
	HasVision bool
	// NeedsRemoteCode is true when the artifact ships *.py modeling modules that
	// require --trust-remote-code (e.g. Nemotron-H).
	NeedsRemoteCode bool
}

// ContractKey is the serving-relevant tuple a launch is validated against. It is
// deliberately NOT bare arch (the qwen3_coder-on-non-Qwen and
// tokenizer-mode-mistral-on-vision failure class): the serving-relevant facts of
// the artifact and the requested mode, crossed with the engine image digest and
// the hardware fingerprint. Any change to a component can invalidate the
// validated flags, which is what the staleness check (v1) and probes (v2) watch.
//
// It is the identity of the VALIDATION CONTRACT (which compatibility verdict
// applies, and the anchor the staleness check compares against), NOT a unique
// fingerprint of a launch invocation. It is intentionally coarse (design §3:
// don't over-key unless a serving-relevant fact varies): runtime launch
// parameters that do not change the compatibility verdict — context length,
// dtype — are deliberately outside the key. Two launches that differ only in
// context length share one contract because they share one compatibility
// verdict. (When v2 needs per-instance identity for the liveness ledger, that is
// an instance id layered on top of this key, not a widening of it — codex #13.)
type ContractKey struct {
	Arch          string          `json:"arch"`
	Tokenizer     TokenizerFamily `json:"tokenizer"`
	Quant         QuantMethod     `json:"quant"`
	Mode          string          `json:"mode"` // canonical sorted-capability label
	EngineDigest  string          `json:"engine_digest"`
	HWFingerprint string          `json:"hw_fingerprint"`
}

// ModeLabel renders a capability set as a stable, order-independent string for
// the contract key's Mode field (so {Thinking,Vision} and {Vision,Thinking}
// key identically). Empty set renders as "base".
func ModeLabel(caps []Capability) string {
	if len(caps) == 0 {
		return "base"
	}
	seen := make(map[Capability]bool, len(caps))
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			out = append(out, string(c))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "+")
}
