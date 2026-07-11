package serveprofiles

import "testing"

func TestLookupHardware_GB10_SeededUnset(t *testing.T) {
	hw, ok := LookupHardware("nvidia:gb10:sm121")
	if !ok {
		t.Fatal("the GB10 accelerator the v1 profiles were authored against must have a hardware profile")
	}
	// The single-instance case must be unchanged: the default is deliberately
	// unset so one model uses the whole box (vLLM's own default wins).
	if hw.GPUMemUtil != 0 {
		t.Errorf("GB10 gpu-mem-util default must be unset (0) to keep single-instance behavior, got %g", hw.GPUMemUtil)
	}
	// It is stamped with the environment it was authored against, exactly as the
	// arch profiles are — so the seam is staleness-checkable.
	if hw.AuthoredAgainst != seededFingerprint {
		t.Errorf("hardware profile must be stamped with the seeded fingerprint, got %+v", hw.AuthoredAgainst)
	}
}

func TestLookupHardware_Unknown_NotFound(t *testing.T) {
	if _, ok := LookupHardware("amd:mi300:gfx942"); ok {
		t.Error("an accelerator with no authored profile must return ok=false so the resolver invents no cap")
	}
	if _, ok := LookupHardware(""); ok {
		t.Error("an empty accelerator must not match a profile")
	}
}

func TestBuiltinHardwareProfiles_IsCopy(t *testing.T) {
	a := BuiltinHardwareProfiles()
	if len(a) == 0 {
		t.Fatal("expected at least one built-in hardware profile")
	}
	a[0].GPUMemUtil = 0.99
	if b := BuiltinHardwareProfiles(); b[0].GPUMemUtil == 0.99 {
		t.Error("BuiltinHardwareProfiles must return a copy; mutating it must not affect the registry")
	}
}
