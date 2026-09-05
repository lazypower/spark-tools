package hardware

import "testing"

func TestAcceleratorFingerprint_StrixHalo(t *testing.T) {
	g := GPUInfo{
		Vendor:   VendorAMD,
		DeviceID: 0x1586,
		Name:     "AMD Radeon 8060S Graphics",
		Compute:  "gfx1151",
	}
	if got, want := AcceleratorFingerprint(g), "amd:strix-halo:gfx1151"; got != want {
		t.Errorf("fingerprint = %q, want %q", got, want)
	}
}

// An AMD part we have not confirmed must still produce a truthful identity.
// Naming the ISA family is honest; inventing a codename is not.
func TestAcceleratorFingerprint_UnknownAMDFallsBackToFamily(t *testing.T) {
	cases := []struct {
		gfx  string
		want string
	}{
		{"gfx942", "amd:cdna3:gfx942"},
		{"gfx90a", "amd:cdna2:gfx90a"},
		{"gfx1100", "amd:rdna3:gfx1100"},
		{"gfx1030", "amd:rdna2:gfx1030"},
		{"gfx1201", "amd:rdna4:gfx1201"},
	}
	for _, c := range cases {
		g := GPUInfo{Vendor: VendorAMD, DeviceID: 0xffff, Compute: c.gfx}
		if got := AcceleratorFingerprint(g); got != c.want {
			t.Errorf("fingerprint(%s) = %q, want %q", c.gfx, got, c.want)
		}
	}
}

func TestAcceleratorFingerprint_NVIDIA(t *testing.T) {
	cases := []struct {
		name, compute, want string
	}{
		// The GB10 identity must round-trip to the string the serving
		// contract was already seeded with, or detection would silently
		// re-stamp every existing DGX Spark artifact.
		{"NVIDIA GB10", "sm_121", "nvidia:gb10:sm121"},
		{"NVIDIA A100-SXM4-40GB", "sm_80", "nvidia:a100:sm80"},
	}
	for _, c := range cases {
		g := GPUInfo{Vendor: VendorNVIDIA, Name: c.name, Compute: c.compute}
		if got := AcceleratorFingerprint(g); got != c.want {
			t.Errorf("fingerprint(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// A GPU we cannot identify must yield "", never a half-known identity that
// would be stamped onto an artifact as if it were verified.
func TestAcceleratorFingerprint_RefusesPartialIdentity(t *testing.T) {
	cases := []GPUInfo{
		{Vendor: VendorAMD, Compute: ""},                         // no gfx target
		{Vendor: VendorNVIDIA, Name: "NVIDIA GB10", Compute: ""}, // no compute cap
		{Vendor: VendorNVIDIA, Name: "", Compute: "sm_121"},      // no chip
		{Vendor: "", Compute: "gfx1151"},                         // no vendor
	}
	for _, g := range cases {
		if got := AcceleratorFingerprint(g); got != "" {
			t.Errorf("unidentifiable GPU %+v produced %q, want empty", g, got)
		}
	}
}

func TestDetectAcceleratorOr_UsesFallbackWhenAbsent(t *testing.T) {
	orig := kfdTopologyRoot
	kfdTopologyRoot = t.TempDir() // no topology, and no nvidia-smi in test env
	defer func() { kfdTopologyRoot = orig }()

	if got := DetectAcceleratorOr(FallbackAccelerator); got != FallbackAccelerator {
		t.Errorf("with no GPU present, expected the fallback %q, got %q", FallbackAccelerator, got)
	}
}

func TestGFXFamily_Unrecognized(t *testing.T) {
	for _, in := range []string{"", "gfx", "gf", "gfx8"} {
		if got := gfxFamily(in); got != "" {
			t.Errorf("gfxFamily(%q) = %q, want empty", in, got)
		}
	}
}
