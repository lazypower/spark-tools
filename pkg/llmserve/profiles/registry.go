package profiles

import (
	"slices"

	"github.com/lazypower/spark-tools/pkg/llmserve/fingerprint"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// seededProvenance records where the v1 claims came from. Per §8.0 every v1
// claim ships `asserted`; the environment they were authored against is
// seededFingerprint, stamped on each profile's AuthoredAgainst.
const seededProvenance = "run.sh MODEL_MAP + AGENTS.md (vllm-config), 2026-06"

// seededFingerprint is the GB10 Spark environment the v1 profiles were authored
// against (AGENTS.md: image v0.23.0, GB10 / SM 12.1). The staleness check warns
// when an operator emits for anything that diverges from this.
var seededFingerprint = fingerprint.Fingerprint{
	Engine:      "vllm/vllm-openai@v0.23.0",
	Accelerator: "nvidia:gb10:sm121",
}

// asserted builds a hand-seeded claim with the v1 default status and provenance.
func asserted(c serving.Capability, supported bool) Claim {
	return Claim{
		Capability: c,
		Supported:  supported,
		Status:     StatusAsserted,
		Provenance: seededProvenance,
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
	// GLM-4.x MoE. Guided decoding; no thinking parser seeded — run.sh serves the
	// GLM models with no reasoning/tool flags (plain NVFP4), so the contract is the
	// same across the MoE and MoE-Lite variants. Glm4MoeLite is GLM-4.7-Flash;
	// Glm4Moe is GLM-4.5/4.6-Air. Both are alt-archs of one contract until a probe
	// (v2) shows they diverge.
	{
		Arch:    "Glm4MoeForCausalLM",
		AltArch: []string{"Glm4MoeLiteForCausalLM"},
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

// init stamps every built-in with the environment it was authored against, so
// the fingerprint lives in one place (seededFingerprint) rather than repeated in
// each literal.
func init() {
	for i := range builtins {
		builtins[i].AuthoredAgainst = seededFingerprint
	}
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
