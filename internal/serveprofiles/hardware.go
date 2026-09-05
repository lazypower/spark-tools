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
	// EnforceEager makes the launch pass vLLM's --enforce-eager, disabling
	// CUDA/HIP graph capture. It is a hardware fact in the strictest sense: on
	// some accelerators graph capture does not merely underperform, it hangs the
	// driver and takes the engine down, so the safe value is a property of the
	// silicon and its runtime rather than of the model or the requested mode.
	//
	// False means UNSET, not "capture is known good" — the flag is simply not
	// emitted and vLLM keeps its own behavior. Only an accelerator where capture
	// is known broken sets this, and it is the one lever here whose default is
	// asserted against a *known engine defect*, which makes the staleness anchor
	// load-bearing: when the defect is fixed the entry should be retired, and the
	// drift warning is what prompts someone to re-check.
	EnforceEager bool
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
		GPUMemUtil:  0,                             // unset: one model uses the whole 121 GB pool
		MaxNumSeqs:  0,                             // unset: defer to vLLM's own concurrency default
	},
	{
		// Strix Halo (Radeon 8060S, RDNA3.5, wave32) with ~62.5 GiB of the
		// 125 GiB system pool carved as unified memory.
		Accelerator: "amd:strix-halo:gfx1151",

		// UNSET, deliberately, and this is the interesting part. A measured
		// 0.70 ran clean on this accelerator, but a value that ran clean is not
		// a measured ceiling: the boundary was never probed upward, and vLLM
		// itself reported headroom at that setting (it suggested a KV cache of
		// 51.33 GiB to "fully utilize gpu memory" against the 43.78 GiB the run
		// actually budgeted). Seeding 0.70 would present an arbitrary safe
		// point as a hardware limit and silently cap every single-instance
		// launch below what the box can do. Encoding a guessed ceiling is the
		// failure mode that OOMs users; leaving it unset keeps vLLM's own
		// default until someone bisects a real boundary.
		GPUMemUtil: 0,
		MaxNumSeqs: 0,

		// UNSET, and this entry is the reason the staleness machinery exists.
		//
		// It was first seeded true on vLLM #32180 (HIP graph capture times out
		// the driver on gfx1151), taken from the issue and from this box's
		// operating notes rather than from a measurement. Against the engine
		// this profile is stamped for, that is no longer true. Measured on
		// gfx1151 / vLLM 0.28.0+strix, serving with capture ENABLED:
		//
		//   Capturing CUDA graphs (PIECEWISE): 51/51, (FULL): 35/35
		//   Graph capturing finished in 8 secs, took 0.45 GiB
		//   Application startup complete — no driver timeout, no crash
		//   output correct (17x3 -> 51; capital of Japan -> Tokyo)
		//   152.2 tok/s median with capture vs 149.6 with --enforce-eager
		//
		// So the flag bought nothing here and cost a little throughput. Forcing
		// it would be a fossil: a requirement that was real against an older
		// engine, carried forward into one where it no longer holds, which is
		// exactly what AuthoredAgainst is meant to catch. An older engine that
		// still needs it will drift against this stamp and warn.
		//
		// The lever itself stays — the failure it guards against is real, just
		// not on this engine build — so an accelerator that needs it can set it
		// without reintroducing the plumbing.
		EnforceEager: false,

		// Stamped against the environment these values were measured on rather
		// than the repo-wide seed, because they were asserted somewhere else
		// entirely — a different vendor, engine build and driver stack.
		AuthoredAgainst: fingerprint.Fingerprint{
			Engine:      "kyuz0/vllm-therock-gfx1151@0.28.0+strix",
			Accelerator: "amd:strix-halo:gfx1151",
		},
	},
}

// init stamps every hardware profile with the environment it was authored
// against, keeping the fingerprint in one place (seededFingerprint) rather than
// repeated in each literal — mirroring the arch-registry stamping.
func init() {
	for i := range hardwareBuiltins {
		// An entry measured on its own hardware carries its own anchor; only
		// the unstamped ones inherit the repo-wide seed.
		if hardwareBuiltins[i].AuthoredAgainst.Zero() {
			hardwareBuiltins[i].AuthoredAgainst = seededFingerprint
		}
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
