package serveprofiles

import "github.com/lazypower/spark-tools/internal/fingerprint"

// HardwareProfile is the serve-side per-accelerator default policy: the launch
// knobs whose safe value is a fact about the physical accelerator, not the model
// or the requested mode. It is the sibling of ArchProfile — keyed on the
// accelerator fingerprint dimension rather than the arch — and fills the seam the
// contract engine documents but never had (the "hardware-profile default applied
// downstream" note on Request). v1 carries exactly one lever: the
// gpu-memory-utilization default the memory budgeter (ADR 0001) sets when it
// places a co-resident instance.
type HardwareProfile struct {
	// Accelerator is the fingerprint.Accelerator dimension this profile keys on
	// (e.g. "nvidia:gb10:sm121").
	Accelerator string
	// AuthoredAgainst is the environment these defaults were asserted against, so
	// the seam is staleness-checked the same anti-fossil way arch profiles are
	// (§8). The engine dimension is the one that matters here:
	// gpu-memory-utilization is a vLLM behavior, so a drifted engine build is a
	// reason to re-verify the default before trusting it.
	AuthoredAgainst fingerprint.Fingerprint
	// GPUMemUtil is the default vLLM --gpu-memory-utilization fraction for this
	// accelerator, in (0, 1]. Zero means UNSET on purpose: the single-instance
	// case should use the whole box (vLLM's own 0.9 default → max KV cache → max
	// context/concurrency), so the cap is an opt-in per-instance override for
	// co-residency, never a silent repo-wide lowering that regresses the common
	// case. An accelerator whose safe default genuinely differs sets a non-zero
	// value here.
	GPUMemUtil float64
	// MaxNumSeqs is the default vLLM --max-num-seqs (max concurrent sequences per
	// iteration) for this accelerator; a positive integer, or zero for UNSET. It
	// is the second budget lever: KV footprint = f(gpu-mem-util, max-model-len,
	// max-num-seqs), so a co-residency budgeter trims concurrency to fit a member
	// alongside others. Unset for the same reason GPUMemUtil is — one model
	// should use vLLM's own default — so this stays additive until the budgeter
	// (ADR 0001) sets it.
	MaxNumSeqs int
}

// hardwareBuiltins is the v1 hardware-profile registry. Today the one accelerator
// the profiles are authored against — GB10 — carries an UNSET gpu-mem-util, so
// bare single-instance bring-up is unchanged. The entry exists so the precedence
// chain (explicit flag > hardware default > vLLM's own default) has a real seam
// to fill and the staleness anchor is stamped in one place, exactly as the arch
// registry does it.
var hardwareBuiltins = []HardwareProfile{
	{
		Accelerator: seededFingerprint.Accelerator, // nvidia:gb10:sm121
		GPUMemUtil:  0,                              // unset: one model uses the whole 121 GB pool
		MaxNumSeqs:  0,                              // unset: defer to vLLM's own concurrency default
	},
}

// init stamps every hardware profile with the environment it was authored
// against, keeping the fingerprint in one place (seededFingerprint) rather than
// repeated in each literal — mirroring the arch-registry stamping.
func init() {
	for i := range hardwareBuiltins {
		hardwareBuiltins[i].AuthoredAgainst = seededFingerprint
	}
}

// BuiltinHardwareProfiles returns a copy of the v1 hardware-profile registry.
func BuiltinHardwareProfiles() []HardwareProfile {
	out := make([]HardwareProfile, len(hardwareBuiltins))
	copy(out, hardwareBuiltins)
	return out
}

// LookupHardware finds the hardware profile for an accelerator fingerprint. ok is
// false when no profile is seeded for the accelerator — the resolver then leaves
// gpu-mem-util unset (deferring to vLLM's own default) rather than inventing a
// cap for an accelerator it has no authored knowledge of.
func LookupHardware(accelerator string) (HardwareProfile, bool) {
	for _, p := range hardwareBuiltins {
		if p.Accelerator == accelerator {
			return p, true
		}
	}
	return HardwareProfile{}, false
}
