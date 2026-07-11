package serveprofiles

import (
	"slices"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serving"
)

// seededProvenance records where the v1 claims came from. Per §8.0 every v1
// claim ships `asserted`; the environment they were authored against is
// seededFingerprint, stamped on each profile's AuthoredAgainst.
const seededProvenance = "run.sh MODEL_MAP + AGENTS.md (vllm-config), 2026-06"

// vlAcceptanceProvenance marks the Qwen3-VL dense entry, whose claims come from a
// real on-box vision acceptance rather than run.sh (which never served a VLM).
// Still `asserted` per §8.0 — a manual accept is strong evidence but the `proven`
// verdict is the v2 probe's to stamp, not the author's.
const vlAcceptanceProvenance = "on-box vision acceptance: Qwen3-VL-32B-Instruct-NVFP4, tensor-warden GB10, vLLM v0.23.0 (2026-07-08)"

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
	// Qwen3.5/3.6 MoE text variants (run.sh qwen-35b / qwen-36b / qwen-36b-fp4 —
	// the latter is the DEFAULT_MODEL). One entry unlocks all three: they report the
	// same arch and differ only by quant, which the probe derives. Same Qwen3
	// reasoning + qwen3_coder tool contract as the Qwen3 MoE family (Qwen tokenizer
	// required, else the tool parser 500s), but TEXT-ONLY — distinct from the
	// vision-capable Qwen3VLMoeForConditionalGeneration (note the absent "VL"), so it
	// carries Vision:false and stands as its own profile rather than a Qwen3Moe alt.
	{
		Arch:                        "Qwen3_5MoeForConditionalGeneration",
		ReasoningParser:             "qwen3",
		ToolCallParser:              "qwen3_coder",
		ToolParserRequiresTokenizer: serving.TokenizerQwen,
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, true),
			asserted(serving.ToolCalling, true),
			asserted(serving.Vision, false),
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
	// Qwen3-VL DENSE (Instruct) — config arch Qwen3VLForConditionalGeneration,
	// model_type qwen3_vl. Distinct from the MoE VL arch
	// (Qwen3VLMoeForConditionalGeneration, an alt of Qwen3Moe): this is the dense
	// VL line (2B/4B/8B/32B). Vision + guided-decoding VALIDATED on-box
	// (tensor-warden GB10, Qwen3-VL-32B-Instruct-NVFP4, vLLM v0.23.0): the
	// multimodal path serves and `response_format` json_schema enforces on it.
	// Client note (not a launch flag): the working structured-output API is
	// `response_format`, NOT the legacy top-level `guided_json` field, which is
	// silently ignored in v0.23.0. Thinking + tool-calling left false: the tested
	// build is the non-thinking Instruct variant and VL tool-calling was not
	// validated (its parser differs from qwen3_coder) — extend when a Thinking or
	// agentic build is accepted. compressed-tensors NVFP4 auto-detected (QuantFlags),
	// no quant flag. Vision is gated by the vision-requires-processor rule; this
	// artifact ships preprocessor_config.json + video_preprocessor_config.json.
	{
		Arch: "Qwen3VLForConditionalGeneration",
		Claims: []Claim{
			{Capability: serving.GuidedDecoding, Supported: true, Status: StatusAsserted, Provenance: vlAcceptanceProvenance},
			{Capability: serving.Thinking, Supported: false, Status: StatusAsserted, Provenance: vlAcceptanceProvenance},
			{Capability: serving.ToolCalling, Supported: false, Status: StatusAsserted, Provenance: vlAcceptanceProvenance},
			{Capability: serving.Vision, Supported: true, Status: StatusAsserted, Provenance: vlAcceptanceProvenance},
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
