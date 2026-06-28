package profiles

import (
	"testing"

	sp "github.com/lazypower/spark-tools/internal/serveprofiles"
	"github.com/lazypower/spark-tools/internal/serving"
)

// The behavior suite lives in internal/serveprofiles; this locks the compat
// surface (alias identity, method ride-along, delegated funcs + data tables).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ sp.ArchProfile = ArchProfile{}
	var _ sp.Claim = Claim{}
	var _ sp.CompatRequest = CompatRequest{}
	var _ sp.CompatRule = CompatRule{}
	var _ sp.ClaimStatus = StatusAsserted
}

func TestWrapper_DelegatedFuncsAndTables(t *testing.T) {
	wp, wok := Lookup("Qwen3MoeForCausalLM")
	ap, aok := sp.Lookup("Qwen3MoeForCausalLM")
	if wok != aok || wp.Arch != ap.Arch {
		t.Error("Lookup must delegate to the authority")
	}
	if len(BuiltinProfiles()) != len(sp.BuiltinProfiles()) {
		t.Error("BuiltinProfiles must delegate")
	}
	wf, wok2 := QuantFlagsFor(serving.QuantGPTQ)
	af, aok2 := sp.QuantFlagsFor(serving.QuantGPTQ)
	if wok2 != aok2 || len(wf) != len(af) {
		t.Error("QuantFlagsFor must delegate")
	}
	if len(CompatRules) != len(sp.CompatRules) || len(QuantFlags) != len(sp.QuantFlags) {
		t.Error("data tables must re-export the authority store")
	}
}
