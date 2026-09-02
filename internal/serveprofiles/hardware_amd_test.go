package serveprofiles

import "testing"

const strixHalo = "amd:strix-halo:gfx1151"

func TestLookupHardware_StrixHalo_RequiresEagerExecution(t *testing.T) {
	hw, ok := LookupHardware(strixHalo)
	if !ok {
		t.Fatal("gfx1151 must have a hardware profile: HIP graph capture crashes the engine there, and that is a fact about the accelerator")
	}
	if !hw.EnforceEager {
		t.Error("gfx1151 must enforce eager execution (vLLM #32180: HIP graph capture times out the driver)")
	}
}

// A value that merely ran clean is not a measured ceiling. Seeding one would
// cap every single-instance launch below what the box can do, which is exactly
// the invented-cap failure the registry's design forbids.
func TestLookupHardware_StrixHalo_BudgetLeversUnset(t *testing.T) {
	hw, _ := LookupHardware(strixHalo)
	if hw.GPUMemUtil != 0 {
		t.Errorf("gfx1151 gpu-mem-util must stay unset until a real boundary is bisected, got %g", hw.GPUMemUtil)
	}
	if hw.MaxNumSeqs != 0 {
		t.Errorf("gfx1151 max-num-seqs must stay unset, got %d", hw.MaxNumSeqs)
	}
}

// The AMD defaults were asserted on a different vendor, engine build and driver
// stack than the repo-wide seed, so they must carry their own anchor -- else a
// drift check would compare them against an environment they never saw.
func TestLookupHardware_StrixHalo_CarriesOwnAuthoredEnvironment(t *testing.T) {
	hw, _ := LookupHardware(strixHalo)
	if hw.AuthoredAgainst == seededFingerprint {
		t.Fatal("the AMD profile must not inherit the GB10 seed fingerprint")
	}
	if hw.AuthoredAgainst.Accelerator != strixHalo {
		t.Errorf("authored accelerator = %q, want %q", hw.AuthoredAgainst.Accelerator, strixHalo)
	}
	if hw.AuthoredAgainst.Engine == "" {
		t.Error("the AMD profile must name the engine build it was measured against")
	}
}

// Stamping the entries must not clobber one that carries its own anchor, and
// must still fill in the ones that do not.
func TestBuiltinHardwareProfiles_StampingPreservesExplicitAnchors(t *testing.T) {
	for _, p := range BuiltinHardwareProfiles() {
		if p.AuthoredAgainst.Zero() {
			t.Errorf("profile %q left unstamped", p.Accelerator)
		}
	}
	gb10, _ := LookupHardware(seededFingerprint.Accelerator)
	if gb10.AuthoredAgainst != seededFingerprint {
		t.Error("an entry with no explicit anchor must still inherit the repo-wide seed")
	}
	// The GB10 path must be untouched by the AMD addition.
	if gb10.EnforceEager {
		t.Error("GB10 must not have acquired enforce-eager")
	}
}
