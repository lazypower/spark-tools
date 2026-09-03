package serveprofiles

import "testing"

const strixHalo = "amd:strix-halo:gfx1151"

// enforce-eager was seeded true from vLLM #32180 without being measured. On the
// engine this profile is stamped for it is simply false: capture completes in 8
// seconds, serves, returns correct output, and is marginally FASTER than eager.
// Forcing it would be a fossil.
func TestLookupHardware_StrixHalo_DoesNotForceEager(t *testing.T) {
	hw, ok := LookupHardware(strixHalo)
	if !ok {
		t.Fatal("gfx1151 must still have a hardware profile")
	}
	if hw.EnforceEager {
		t.Error("gfx1151 must not force eager: measured on vLLM 0.28.0+strix, HIP graph capture completes and serves correctly")
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
