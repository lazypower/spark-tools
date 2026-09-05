package hardware

import "strings"

// Accelerator fingerprints are the "vendor:arch:compute" identity the serving
// contract stamps artifacts with (internal/fingerprint). The shape is
// deliberately not CUDA-specific -- "nvidia:gb10:sm121" and
// "amd:strix-halo:gfx1151" are the same three dimensions: who made it, which
// chip it is, and which kernels it wants.

// FallbackAccelerator is the identity used when nothing can be detected. It
// preserves the historical llm-serve default so a box that cannot introspect
// its accelerator behaves exactly as it did before detection existed.
const FallbackAccelerator = "nvidia:gb10:sm121"

// amdCodenames maps a PCI device ID to the chip codename used as the arch
// dimension, for devices confirmed on real hardware.
var amdCodenames = map[int64]string{
	0x1586: "strix-halo",
}

// DetectAccelerator returns the fingerprint of the first detected GPU, or ""
// when no GPU is present. Callers that need a value regardless should use
// DetectAcceleratorOr.
func DetectAccelerator() string {
	hw, err := DetectHardware()
	if err != nil || hw == nil || len(hw.GPUs) == 0 {
		return ""
	}
	return AcceleratorFingerprint(hw.GPUs[0])
}

// DetectAcceleratorOr returns the detected accelerator fingerprint, falling
// back to fallback when detection finds nothing.
func DetectAcceleratorOr(fallback string) string {
	if a := DetectAccelerator(); a != "" {
		return a
	}
	return fallback
}

// AcceleratorFingerprint renders a GPU as its "vendor:arch:compute" identity.
// It returns "" for a GPU it cannot identify, so a caller never stamps an
// artifact with a half-known identity.
func AcceleratorFingerprint(g GPUInfo) string {
	switch g.Vendor {
	case VendorAMD:
		return amdFingerprint(g)
	case VendorNVIDIA:
		return nvidiaFingerprint(g)
	default:
		return ""
	}
}

// amdFingerprint builds e.g. "amd:strix-halo:gfx1151". The compute dimension is
// the gfx target, which is exactly what the ROCm toolchain compiles against.
func amdFingerprint(g GPUInfo) string {
	gfx := strings.ToLower(g.Compute)
	if gfx == "" {
		return ""
	}
	arch := amdArch(g)
	return VendorAMD + ":" + arch + ":" + gfx
}

// amdArch names the chip. A confirmed device ID gives the codename; otherwise
// the gfx target's family is a truthful, coarser answer ("rdna3") and is far
// better than inventing a codename we have not verified.
func amdArch(g GPUInfo) string {
	if name, ok := amdCodenames[g.DeviceID]; ok {
		return name
	}
	if fam := gfxFamily(g.Compute); fam != "" {
		return fam
	}
	return "unknown"
}

// gfxFamily maps a gfx target to its architecture family. The mapping is by
// major/minor because that is what the ISA generation actually tracks.
func gfxFamily(gfx string) string {
	digits := strings.TrimPrefix(strings.ToLower(gfx), "gfx")
	if len(digits) < 3 {
		return ""
	}
	switch {
	case strings.HasPrefix(digits, "12"):
		return "rdna4"
	case strings.HasPrefix(digits, "115"), strings.HasPrefix(digits, "11"):
		return "rdna3"
	case strings.HasPrefix(digits, "103"), strings.HasPrefix(digits, "10"):
		return "rdna2"
	case strings.HasPrefix(digits, "94"):
		return "cdna3"
	case strings.HasPrefix(digits, "90"):
		return "cdna2"
	}
	return ""
}

// nvidiaFingerprint builds e.g. "nvidia:gb10:sm121" from the reported name and
// compute capability. nvidia-smi names the part "NVIDIA GB10"; the fingerprint
// wants just the chip, and the compute dimension carries no underscore.
func nvidiaFingerprint(g GPUInfo) string {
	compute := strings.ReplaceAll(strings.ToLower(g.Compute), "_", "")
	chip := nvidiaChip(g.Name)
	if chip == "" || compute == "" {
		return ""
	}
	return VendorNVIDIA + ":" + chip + ":" + compute
}

// nvidiaChip reduces a marketing name to the chip token used as the arch
// dimension: "NVIDIA GB10" -> "gb10", "NVIDIA A100-SXM4-40GB" -> "a100".
func nvidiaChip(name string) string {
	f := strings.Fields(strings.ToLower(name))
	for _, tok := range f {
		if tok == "nvidia" {
			continue
		}
		// Trim SKU suffixes: "a100-sxm4-40gb" -> "a100".
		if i := strings.IndexByte(tok, '-'); i > 0 {
			tok = tok[:i]
		}
		if tok != "" {
			return tok
		}
	}
	return ""
}
