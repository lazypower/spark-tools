package profiles

import (
	"slices"

	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// seededProvenance records where the v1 claims came from and the engine
// fingerprint they were authored against. Per §8.0 every v1 claim ships
// `asserted` against this fingerprint; the staleness check warns when a target
// emit's fingerprint diverges from it.
const (
	seededProvenance    = "run.sh MODEL_MAP + AGENTS.md (vllm-config), 2026-06"
	seededAgainstEngine = "vllm/vllm-openai@v0.23.0"
)

// asserted builds a hand-seeded claim with the v1 default status and provenance.
func asserted(c serving.Capability, supported bool) Claim {
	return Claim{
		Capability:    c,
		Supported:     supported,
		Status:        StatusAsserted,
		Provenance:    seededProvenance,
		ProvenAgainst: seededAgainstEngine,
	}
}

// builtins is the v1 arch-profile registry: the ~6 archs run.sh serves, seeded
// from the working oracle. Built-in for v1, user-overridable later (decision §7.2,
// mirrors the hfetch profile precedent).
var builtins = []ArchProfile{
	// Qwen3 MoE (Qwen3.5/3.6 thinking + coder variants). Same arch serves both the
	// thinking model (reasoning qwen3) and the coder model (no thinking, guided
	// decoding on) — the variants differ by requested MODE, not arch, which is
	// exactly why the contract key includes mode and not just arch. The qwen3_coder
	// tool parser requires a Qwen tokenizer (500s otherwise).
	{
		Arch:                        "Qwen3MoeForCausalLM",
		AltArch:                     []string{"Qwen3NextForCausalLM", "Qwen3VLMoeForConditionalGeneration"},
		ReasoningParser:             "qwen3",
		ToolCallParser:              "qwen3_coder",
		ToolParserRequiresTokenizer: serving.TokenizerQwen,
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, true),
			asserted(serving.ToolCalling, true),
			asserted(serving.Vision, true), // Qwen3.6 vision
		},
	},
	// Nemotron-H — trust-remote-code (ships *.py modeling modules); reasoning via
	// the nano_v3 parser plugin. No Qwen tool parser.
	{
		Arch:            "NemotronHForCausalLM",
		ReasoningParser: "nano_v3",
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, true),
			asserted(serving.ToolCalling, false),
			asserted(serving.Vision, false),
		},
	},
	// GLM-4.x MoE (GLM-4.7-Flash NVFP4). Guided decoding; no thinking parser seeded.
	{
		Arch: "Glm4MoeForCausalLM",
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, false),
			asserted(serving.ToolCalling, false),
			asserted(serving.Vision, false),
		},
	},
	// Mistral3 / Pixtral vision (Devstral, Mistral3). Tekken tokenizer selects
	// --tokenizer-mode mistral, which crashes on the vision path — the
	// tokenizer-mode ⊗ vision negative-compat rule guards exactly this arch.
	{
		Arch: "Mistral3ForConditionalGeneration",
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, false),
			asserted(serving.ToolCalling, false),
			asserted(serving.Vision, true),
		},
	},
}

// BuiltinProfiles returns a copy of the v1 built-in arch-profile registry.
func BuiltinProfiles() []ArchProfile {
	out := make([]ArchProfile, len(builtins))
	copy(out, builtins)
	return out
}

// Lookup finds the profile for an architecture string, matching the canonical
// Arch or any AltArch. ok is false when no profile is seeded for the arch — the
// resolver must reject an unknown arch rather than emit an unvalidated launch.
func Lookup(arch string) (ArchProfile, bool) {
	for _, p := range builtins {
		if p.Arch == arch || slices.Contains(p.AltArch, arch) {
			return p, true
		}
	}
	return ArchProfile{}, false
}
