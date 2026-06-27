// Package profiles is the declarative arch-profile registry: the hard-won
// per-architecture serving knowledge that run.sh's MODEL_MAP and AGENTS.md
// encode by hand, promoted to data. An ArchProfile says what flags an arch's
// capabilities realize and what the arch claims to support; the negative-compat
// rules say which combinations silently break. Adding an arch is one declarative
// entry, never a rewrite (§2 pluggable-seam requirement).
//
// Following the §8.0 claims-vs-status split: claims are hand-seeded hypotheses
// (this package), status is probe-generated (v2). v1 ships every claim stamped
// `asserted` against the fingerprint it was authored on.
package profiles

import "github.com/lazypower/spark-tools/pkg/llmserve/serving"

// ClaimStatus is the lifecycle of a capability claim (§8.0). Status is never
// hand-asserted beyond the initial `asserted`; the probe subsystem (v2) promotes
// it to `proven` or flags `drifted`. Defining the three states now keeps the
// data shape stable when probes land — they change trust, not schema.
type ClaimStatus string

const (
	// StatusAsserted is a hand-seeded hypothesis — the v1 default for every claim.
	StatusAsserted ClaimStatus = "asserted"
	// StatusProven means a probe confirmed the claim against ProvenAgainst (v2).
	StatusProven ClaimStatus = "proven"
	// StatusDrifted means a probe contradicted the claim — re-verify (v2).
	StatusDrifted ClaimStatus = "drifted"
)

// Claim is a hand-seeded hypothesis that an arch supports a capability, carrying
// provenance and the fingerprint it was authored against (§8.0). The verdict
// (Status) starts `asserted` and is only ever changed by probes, never by hand.
type Claim struct {
	Capability    serving.Capability `json:"capability"`
	Supported     bool               `json:"supported"`
	Status        ClaimStatus        `json:"status"`
	Provenance    string             `json:"provenance"`     // where the claim came from
	ProvenAgainst string             `json:"proven_against"` // fingerprint authored against
}

// ArchProfile is the serving contract for one architecture. It carries the
// parser names a capability realizes, the artifact properties that gate them,
// and the capability claims. It does NOT carry quant→flag mappings: that is a
// fact about vLLM, not an arch, so it has a single authority in QuantFlags.
type ArchProfile struct {
	// Arch is the canonical config.json architectures[0] this profile keys on.
	Arch string
	// AltArch are other architecture strings that resolve to this same profile.
	AltArch []string
	// ReasoningParser is the --reasoning-parser value for the Thinking capability
	// (e.g. "qwen3", "nano_v3"). Empty means the arch cannot emit reasoning.
	ReasoningParser string
	// ToolCallParser is the --tool-call-parser value for the ToolCalling
	// capability (e.g. "qwen3_coder"). Empty means no tool parser.
	ToolCallParser string
	// ToolParserRequiresTokenizer, when set, gates the tool parser on the
	// artifact's tokenizer family — qwen3_coder 500s on non-Qwen tokenizers.
	ToolParserRequiresTokenizer serving.TokenizerFamily
	// Claims are the hand-seeded capability hypotheses for this arch.
	Claims []Claim
}

// Supports reports whether the profile claims a capability (regardless of
// status). A capability with no claim is treated as unsupported.
func (p ArchProfile) Supports(c serving.Capability) bool {
	for _, cl := range p.Claims {
		if cl.Capability == c {
			return cl.Supported
		}
	}
	return false
}

// QuantFlags is the single authority for translating a detected quant method to
// the vLLM launch flags it requires (the §1 hard-won knowledge). NVFP4 /
// compressed-tensors / FP8 are auto-detected and need no flag; GPTQ-Int4 on an
// MoE arch needs --quantization moe_wna16 or it loads wrong.
//
// This is global because it is a fact about the engine, not an architecture —
// collapsing it here keeps one source of truth (a method missing from this map
// is a known-incomplete signal, surfaced by QuantFlagsFor).
var QuantFlags = map[serving.QuantMethod][]string{
	serving.QuantNone:              nil,
	serving.QuantNVFP4:             nil,
	serving.QuantFP8:               nil,
	serving.QuantCompressedTensors: nil,
	serving.QuantGPTQ:              {"--quantization", "moe_wna16"},
}

// QuantFlagsFor returns the launch flags for a quant method and whether the
// method is known. An unknown method (ok=false) must be rejected at resolution,
// not silently launched with no flag — a future quant family that needs a flag
// would otherwise load wrong silently.
func QuantFlagsFor(q serving.QuantMethod) (flags []string, ok bool) {
	flags, ok = QuantFlags[q]
	return flags, ok
}
