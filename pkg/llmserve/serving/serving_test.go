package serving

import (
	"testing"

	iserving "github.com/lazypower/spark-tools/internal/serving"
)

// The behavior suite lives in internal/serving; this locks the compat surface
// (alias identity + method ride-along + delegated funcs/vars).

func TestWrapper_AliasIdentity(t *testing.T) {
	// Values flow across the boundary as the SAME type.
	var _ iserving.ContractKey = ContractKey{}
	var _ iserving.Capability = GuidedDecoding
	var _ iserving.QuantMethod = QuantNVFP4
	var _ iserving.TokenizerFamily = TokenizerQwen
	var _ iserving.ArtifactFacts = ArtifactFacts{}
}

func TestWrapper_MethodRideAlong(t *testing.T) {
	k := ContractKey{Arch: "Qwen3MoeForCausalLM", Mode: "base"}
	if k.Canonical() != (iserving.ContractKey{Arch: "Qwen3MoeForCausalLM", Mode: "base"}).Canonical() {
		t.Error("aliased ContractKey.Canonical must match the authority")
	}
}

func TestWrapper_DelegatedFuncsAndVars(t *testing.T) {
	if ModeLabel([]Capability{Vision, Thinking}) != iserving.ModeLabel([]iserving.Capability{Vision, Thinking}) {
		t.Error("ModeLabel must delegate to the authority")
	}
	if len(AllCapabilities) != len(iserving.AllCapabilities) {
		t.Error("AllCapabilities must re-export the authority slice")
	}
}
