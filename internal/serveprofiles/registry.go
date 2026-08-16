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

// qwen35VisionProvenance backs the Qwen3.5/3.6 vision claims. The evidence is
// artifact metadata, not a serving acceptance: config.json for both the dense
// and MoE builds carries vision_config, image_token_id, video_token_id and
// vision_start/end_token_id, and the artifacts ship preprocessor_config.json,
// processor_config.json and video_preprocessor_config.json. Weaker than
// vlAcceptanceProvenance — the multimodal path has not been exercised on this
// engine. The claim says the ARCH is multimodal; whether a given build ships the
// processor stays the vision-requires-processor rule's call at resolution.
const qwen35VisionProvenance = "artifact metadata: Qwen3.6 config.json vision_config + preprocessor/video_preprocessor configs (GB10 acceptance report, 2026-08-11); vision path not yet served on-box"

// qwen35DenseProvenance backs the non-vision claims of the dense Qwen3.5/3.6
// entry, inferred from its MoE sibling: the two lines share the Qwen3.5/3.6 chat
// template and the same reasoning/tool parser contract. The dense arch has not
// been served on this box at all, which is why it does not borrow the MoE
// entry's run.sh provenance.
const qwen35DenseProvenance = "family inference from Qwen3_5MoeForConditionalGeneration (same Qwen3.5/3.6 chat template + qwen3/qwen3_coder parser contract); dense arch not yet served on-box"

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
	// Qwen3.5/3.6 MoE variants (qwen-35b / qwen-36b / qwen-36b-fp4). One entry
	// unlocks all three: they report the same arch and differ only by quant, which
	// the probe derives. Same Qwen3 reasoning + qwen3_coder tool contract as the
	// Qwen3 MoE family (Qwen tokenizer required, else the tool parser 500s).
	//
	// Vision was originally seeded false here on the reading that the absent "VL"
	// in the arch name marked a text-only line, as it does for
	// Qwen3VLMoeForConditionalGeneration. That reading was wrong: the Qwen3.5/3.6
	// generation is natively multimodal, the ForConditionalGeneration suffix is
	// itself the multimodal marker (a text-only Qwen arch is ForCausalLM), and the
	// artifacts carry the vision config and processors. Corrected to true on the
	// artifact evidence — see qwen35VisionProvenance for what that evidence is and
	// is not.
	{
		Arch:                        "Qwen3_5MoeForConditionalGeneration",
		ReasoningParser:             "qwen3",
		ToolCallParser:              "qwen3_coder",
		ToolParserRequiresTokenizer: serving.TokenizerQwen,
		Claims: []Claim{
			asserted(serving.GuidedDecoding, true),
			asserted(serving.Thinking, true),
			asserted(serving.ToolCalling, true),
			{Capability: serving.Vision, Supported: true, Status: StatusAsserted, Provenance: qwen35VisionProvenance},
		},
	},
	// Qwen3.5/3.6 DENSE — config arch Qwen3_5ForConditionalGeneration (Qwen3.6-27B).
	// The dense sibling of the MoE entry above, exactly as
	// Qwen3VLForConditionalGeneration is the dense sibling of the VL MoE arch: same
	// generation, same chat template, same parser contract, different expert
	// topology. Kept as its own profile rather than an AltArch of the MoE entry
	// because the claims carry different provenance — the MoE line is seeded from a
	// working oracle, this one is inferred and unserved — and a probe (v2) will
	// prove or drift the two independently.
	//
	// The NVFP4 build of this arch is ModelOpt mixed precision, not uniform NVFP4;
	// that is a quant fact, resolved through QuantFlags, not an arch fact.
	{
		Arch:                        "Qwen3_5ForConditionalGeneration",
		ReasoningParser:             "qwen3",
		ToolCallParser:              "qwen3_coder",
		ToolParserRequiresTokenizer: serving.TokenizerQwen,
		Claims: []Claim{
			{Capability: serving.GuidedDecoding, Supported: true, Status: StatusAsserted, Provenance: qwen35DenseProvenance},
			{Capability: serving.Thinking, Supported: true, Status: StatusAsserted, Provenance: qwen35DenseProvenance},
			{Capability: serving.ToolCalling, Supported: true, Status: StatusAsserted, Provenance: qwen35DenseProvenance},
			{Capability: serving.Vision, Supported: true, Status: StatusAsserted, Provenance: qwen35VisionProvenance},
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
