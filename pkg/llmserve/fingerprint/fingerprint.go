// Package fingerprint identifies the environment a serving contract was proven
// (or asserted) against. It is the anchor of the v1 anti-fossil posture (§8):
// every requirement we encode is a fact about (engine build × accelerator), not
// a timeless law — --enforce-eager was required on v0.19 and obsolete on v0.23 —
// so a profile's flags are trustworthy only while the target environment matches
// the one they were authored against. v1 does not probe; it does the cheap
// check: compare the target-emit fingerprint (computable pre-launch) against the
// stamped one and warn loudly when they diverge ("asserted + stale — re-verify").
//
// The fingerprint is shaped accelerator-general NOW (codex #10/#14: vendor+arch,
// never CUDA-only hw_sm) because it is expensive to retrofit. v1 keys on the two
// dimensions an emit can know offline — engine image and accelerator — and the
// design §8 set (driver, container runtime, model/tokenizer/remote-code revision)
// slots in as added fields here without changing call sites.
package fingerprint

import "strings"

// Fingerprint is the environment a contract is trusted against. Engine is the
// vLLM image digest/tag (e.g. "vllm/vllm-openai@v0.23.0"); Accelerator is the
// vendor+arch identity (e.g. "nvidia:gb10:sm121"), deliberately not a CUDA-only
// SM number so a non-CUDA accelerator slots in.
type Fingerprint struct {
	Engine      string `json:"engine"`
	Accelerator string `json:"accelerator"`
}

// Zero reports whether the fingerprint has no dimensions set. A zero fingerprint
// cannot anchor a staleness check, so the contract engine refuses to stamp one.
func (f Fingerprint) Zero() bool {
	return f.Engine == "" && f.Accelerator == ""
}

// Canonical renders a stable string form for stamping and comparison.
func (f Fingerprint) Canonical() string {
	return "engine=" + f.Engine + ";accel=" + f.Accelerator
}

// Drift is the set of dimensions where a target environment diverges from the
// stamped one. Empty means the target matches what the contract was authored
// against (no staleness warning). Each entry is a human-readable "dim: stamped →
// target" so the operator sees exactly what changed.
func Drift(target, stamped Fingerprint) []string {
	var d []string
	if target.Engine != stamped.Engine {
		d = append(d, "engine: "+orNone(stamped.Engine)+" → "+orNone(target.Engine))
	}
	if target.Accelerator != stamped.Accelerator {
		d = append(d, "accelerator: "+orNone(stamped.Accelerator)+" → "+orNone(target.Accelerator))
	}
	return d
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
