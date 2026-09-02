// Package servecontract resolves a serve request against the arch profiles into a
// validated vLLM launch spec: it realizes requested capabilities as backend
// flags, rejects incompatible combinations (the negative-compat rules), and
// stamps the contract key. This is the (A) contract engine — the value-density
// slice. It stops at producing a validated, ordered flag set; rendering that to
// a compose/docker-run/quadlet spec is the emit driver's job, and launching it
// is v2 (B). Resolution is a pure function of (request, artifact facts), so it
// is fully unit-testable off the GPU.
package servecontract

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serveprofiles"
	"github.com/lazypower/spark-tools/internal/serving"
)

// Request is a launch request: a model (resolved to verified artifact facts by
// the hfetch boundary), the capabilities the caller wants, the context length,
// and the hardware/engine context the launch targets. It expresses intent, not
// flags (§2).
type Request struct {
	// ServedName is the friendly alias the model is served under (the canonical
	// id in /v1/models). Required.
	ServedName string
	// Capabilities are the requested serving capabilities.
	Capabilities []serving.Capability
	// ContextLen is the requested max model length (tokens). Zero means use the
	// hardware-profile default applied downstream; resolution leaves it unset.
	ContextLen int
	// GPUMemUtil is the requested vLLM --gpu-memory-utilization fraction, in
	// (0, 1]. Zero means unset: resolution falls back to the accelerator's
	// hardware-profile default (serveprofiles.LookupHardware), and if that is also
	// unset the flag is omitted so vLLM applies its own default (0.9 — one model
	// uses the whole box). This is the single memory-budget lever (ADR 0001): a
	// per-instance cap a co-residency budgeter sets so N instances sum to < 1 on
	// the unified pool. A set fraction outside (0, 1] is rejected fail-closed.
	GPUMemUtil float64
	// MaxNumSeqs is the requested vLLM --max-num-seqs (max concurrent sequences
	// per iteration). Zero means unset: resolution falls back to the accelerator's
	// hardware-profile default, and if that is also unset the flag is omitted so
	// vLLM applies its own default. It is the second budget lever alongside
	// GPUMemUtil (KV footprint = f(gpu-mem-util, max-model-len, max-num-seqs)): a
	// budgeter trims concurrency to fit a co-resident member. A set value below 1
	// is rejected fail-closed.
	MaxNumSeqs int
	// Dtype is the vLLM --dtype value; empty defaults to "auto".
	Dtype string
	// Target is the environment the launch is being emitted for (engine image +
	// accelerator). It supplies the contract key's engine/hardware dimensions and
	// is compared against the profile's authored fingerprint for the staleness
	// warning. Both dimensions are required (an un-fingerprinted emit cannot be
	// staleness-checked).
	Target fingerprint.Fingerprint
}

// Resolved is a validated launch contract: the contract key it was validated
// against, the ordered vLLM flags that realize the request, and any staleness
// warnings (the warn-not-gate posture — loud, but not a hard gate in v1).
// Rendering to a concrete launch spec is the emit driver's responsibility.
type Resolved struct {
	Key      serving.ContractKey
	Flags    []string
	Warnings []string
}

// RejectionError is returned when a request cannot be served safely: an unknown
// arch, an unknown quant method, or a negative-compat rule violation. It names
// what failed and how to fix it so the operator gets actionable guidance, never
// a silent footgun launch.
type RejectionError struct {
	Rule   string // the rule or check that rejected the request
	Reason string // what is wrong
	Remedy string // how to fix it
}

func (e *RejectionError) Error() string {
	if e.Remedy == "" {
		return fmt.Sprintf("%s: %s", e.Rule, e.Reason)
	}
	return fmt.Sprintf("%s: %s — %s", e.Rule, e.Reason, e.Remedy)
}

// AsRejection extracts a *RejectionError from an error chain, if present.
func AsRejection(err error) (*RejectionError, bool) {
	var re *RejectionError
	if errors.As(err, &re) {
		return re, true
	}
	return re, false
}

// Resolve validates a request against the artifact facts and the arch profiles,
// returning the validated launch contract or a *RejectionError. The pipeline
// (design §3) stops at EMIT-ready flags:
//  1. look up the arch profile (reject unknown arch)
//  2. validate the quant method is known (reject unknown quant)
//  3. validate every requested capability is claimed by the profile
//  4. check the negative-compat rules (reject incompatible combos)
//  5. realize capabilities + quant + artifact facts into ordered flags
//  6. compose the contract key
func Resolve(req Request, facts serving.ArtifactFacts) (*Resolved, error) {
	if req.ServedName == "" {
		return nil, &RejectionError{Rule: "request", Reason: "served name is required"}
	}
	if facts.ModelPath == "" {
		return nil, &RejectionError{Rule: "request", Reason: "artifact has no resolved model path (resolve through hfetch first)"}
	}
	// The contract key is meaningless without the engine and hardware dimensions:
	// they are what the staleness check compares a future emit against. An emit
	// stamped with an empty fingerprint cannot be re-verified, so reject it here
	// rather than emit an un-stampable contract.
	if req.Target.Engine == "" {
		return nil, &RejectionError{Rule: "request", Reason: "target engine image digest is required to stamp the contract key"}
	}
	if req.Target.Accelerator == "" {
		return nil, &RejectionError{Rule: "request", Reason: "target accelerator fingerprint is required to stamp the contract key"}
	}

	// 1. Arch profile must exist — an unknown arch has no validated contract.
	profile, ok := serveprofiles.Lookup(facts.Arch)
	if !ok {
		return nil, &RejectionError{
			Rule:   "unknown-arch",
			Reason: fmt.Sprintf("no serving profile for architecture %q", facts.Arch),
			Remedy: "add an arch profile entry, or serve a supported architecture",
		}
	}

	// 2. Quant method must be known — an unknown method might silently load wrong.
	quantFlags, ok := serveprofiles.QuantFlagsFor(facts.Quant)
	if !ok {
		return nil, &RejectionError{
			Rule:   "unknown-quant",
			Reason: fmt.Sprintf("no flag policy for quant method %q", facts.Quant),
			Remedy: "add the quant method to profiles.QuantFlags",
		}
	}

	// 3. Every requested capability must be claimed by the profile.
	for _, c := range req.Capabilities {
		if !profile.Supports(c) {
			return nil, &RejectionError{
				Rule:   "unsupported-capability",
				Reason: fmt.Sprintf("architecture %q does not support %q", facts.Arch, c),
				Remedy: "drop the capability, or serve a model whose arch supports it",
			}
		}
	}

	// 4. Negative-compat rules — reject footgun combinations.
	creq := serveprofiles.CompatRequest{Capabilities: req.Capabilities, Facts: facts, Profile: profile}
	for _, rule := range serveprofiles.CompatRules {
		if bad, reason := rule.Violated(creq); bad {
			return nil, &RejectionError{Rule: rule.Name, Reason: reason, Remedy: rule.Remedy}
		}
	}

	// 4b. Resolve the memory-budget levers by precedence (explicit flag > hardware
	// default > vLLM's own default) and validate them. A bad budget must fail
	// closed here, never silently overcommit into an OOM.
	gpuMemUtil, gpuWarnings, err := resolveGPUMemUtil(req)
	if err != nil {
		return nil, err
	}
	maxNumSeqs, seqWarnings, err := resolveMaxNumSeqs(req)
	if err != nil {
		return nil, err
	}
	enforceEager, eagerWarnings := resolveEnforceEager(req)

	// 5. Realize flags.
	flags := assembleFlags(req, facts, profile, quantFlags, gpuMemUtil, maxNumSeqs, enforceEager)

	// 6. Contract key.
	key := serving.ContractKey{
		Arch:          facts.Arch,
		Tokenizer:     facts.Tokenizer,
		Quant:         facts.Quant,
		Mode:          serving.ModeLabel(req.Capabilities),
		EngineDigest:  req.Target.Engine,
		HWFingerprint: req.Target.Accelerator,
	}

	// 7. Staleness warning (warn-not-gate, §8.0). The flags are asserted against
	// the profile's authored environment; if the operator is emitting for a
	// different engine/accelerator, the assertions may no longer hold (e.g.
	// enforce-eager need, native FP4, structured outputs). Warn loudly and
	// datedly — but do not block: v1 is emit + human-in-loop, and the hard
	// pre-serve gate lands only when v2 owns automated launch.
	var warnings []string
	if drift := fingerprint.Drift(req.Target, profile.AuthoredAgainst); len(drift) > 0 {
		warnings = append(warnings, stalenessWarning(facts.Arch, profile.AuthoredAgainst, drift))
	}
	// The memory-budget defaults carry their own authored environment; when one
	// was the source of the value and the target has drifted, name that specific
	// concern (a distinct authored assumption from the arch flags above).
	warnings = append(warnings, gpuWarnings...)
	warnings = append(warnings, seqWarnings...)
	warnings = append(warnings, eagerWarnings...)

	return &Resolved{Key: key, Flags: flags, Warnings: warnings}, nil
}

// resolveGPUMemUtil applies the memory-budget precedence — an explicit request
// value wins; else the accelerator's hardware-profile default; else unset (0),
// deferring to vLLM's own default — and validates that any *set* fraction is in
// (0, 1]. Returning 0 means "emit no --gpu-memory-utilization flag." When the
// value is sourced from a hardware default whose authored environment has
// drifted, it also returns the loud re-verify warning for that specific knob.
func resolveGPUMemUtil(req Request) (util float64, warnings []string, err error) {
	if req.GPUMemUtil != 0 {
		// NaN must be rejected explicitly: it is != 0 (so it is "set"), yet every
		// ordered comparison against it is false, so a bare `< 0 || > 1` range test
		// would let it through — and then `> 0` in assembleFlags is also false, so
		// the cap silently vanishes and vLLM reverts to 0.9. That is a fail-OPEN
		// budget (co-resident OOM), so a non-finite fraction fails closed here.
		if v := req.GPUMemUtil; math.IsNaN(v) || v < 0 || v > 1 {
			return 0, nil, &RejectionError{
				Rule:   "gpu-mem-util-range",
				Reason: fmt.Sprintf("gpu memory utilization %g is not a fraction in (0, 1]", v),
				Remedy: "pass a finite fraction in (0, 1] — this instance's share of the unified memory pool; on shared memory the co-resident set must sum to < 1",
			}
		}
		return req.GPUMemUtil, nil, nil
	}
	hw, ok := serveprofiles.LookupHardware(req.Target.Accelerator)
	if !ok || hw.GPUMemUtil == 0 {
		return 0, nil, nil // no hardware default → leave unset (vLLM's own default)
	}
	if drift := fingerprint.Drift(req.Target, hw.AuthoredAgainst); len(drift) > 0 {
		warnings = append(warnings, hwStalenessWarning("gpu-memory-utilization", req.Target.Accelerator, hw.AuthoredAgainst, drift))
	}
	return hw.GPUMemUtil, warnings, nil
}

// resolveMaxNumSeqs applies the same precedence as resolveGPUMemUtil to the
// second budget lever — explicit request value > accelerator hardware default >
// unset (0), deferring to vLLM's own default — and validates that a set value is
// a positive count. Returning 0 means "emit no --max-num-seqs flag." A drifted
// hardware default carries the loud re-verify warning for that knob.
func resolveMaxNumSeqs(req Request) (n int, warnings []string, err error) {
	if req.MaxNumSeqs != 0 {
		if req.MaxNumSeqs < 0 {
			return 0, nil, &RejectionError{
				Rule:   "max-num-seqs-range",
				Reason: fmt.Sprintf("max num seqs %d is not a positive count", req.MaxNumSeqs),
				Remedy: "pass a positive integer — the max concurrent sequences per iteration; lower it to shrink a co-resident member's KV footprint",
			}
		}
		return req.MaxNumSeqs, nil, nil
	}
	hw, ok := serveprofiles.LookupHardware(req.Target.Accelerator)
	if !ok || hw.MaxNumSeqs == 0 {
		return 0, nil, nil // no hardware default → leave unset (vLLM's own default)
	}
	if drift := fingerprint.Drift(req.Target, hw.AuthoredAgainst); len(drift) > 0 {
		warnings = append(warnings, hwStalenessWarning("max-num-seqs", req.Target.Accelerator, hw.AuthoredAgainst, drift))
	}
	return hw.MaxNumSeqs, warnings, nil
}

// resolveEnforceEager reports whether the target accelerator requires eager
// execution. Unlike the budget levers there is no request-side override: this is
// not a preference the caller tunes but a statement about what the accelerator's
// runtime survives, and an operator who needs to override it can drop the
// accelerator's hardware profile rather than silently re-enable a known crash.
//
// The flag is still emitted when the authored environment has drifted, with the
// re-verify warning attached. Eager execution is the fail-safe direction — it
// costs throughput, while the alternative is an engine that dies — so drift
// prompts a human to re-check rather than silently withdrawing the guard.
func resolveEnforceEager(req Request) (bool, []string) {
	hw, ok := serveprofiles.LookupHardware(req.Target.Accelerator)
	if !ok || !hw.EnforceEager {
		return false, nil
	}
	var warnings []string
	if drift := fingerprint.Drift(req.Target, hw.AuthoredAgainst); len(drift) > 0 {
		warnings = append(warnings, hwStalenessWarning("enforce-eager", req.Target.Accelerator, hw.AuthoredAgainst, drift))
	}
	return true, warnings
}

// stalenessWarning renders the loud, dated "asserted + stale — re-verify" notice
// for a profile whose authored environment differs from the emit target.
func stalenessWarning(arch string, stamped fingerprint.Fingerprint, drift []string) string {
	return fmt.Sprintf(
		"asserted + stale — re-verify: profile %q was asserted against %s, but you are emitting for a different environment (%s). "+
			"The validated flags may not hold here — re-check enforce-eager need, native FP4, and structured outputs against the target before trusting this launch.",
		arch, stamped.Canonical(), strings.Join(drift, "; "),
	)
}

// hwStalenessWarning renders the re-verify notice for a hardware-profile default
// (the named vLLM knob) whose authored environment differs from the emit target —
// a memory-sizing default is only trustworthy while the engine build it was
// asserted against still holds.
func hwStalenessWarning(knob, accelerator string, stamped fingerprint.Fingerprint, drift []string) string {
	return fmt.Sprintf(
		"asserted + stale — re-verify: the %s default for accelerator %q was asserted against %s, "+
			"but you are emitting for a different environment (%s). Re-check the memory budget against the target before trusting this value.",
		knob, accelerator, stamped.Canonical(), strings.Join(drift, "; "),
	)
}

// assembleFlags builds the ordered vLLM flag list from the validated request.
// Order mirrors the working oracle's compose command so an emitted spec reads
// like the hand-rolled one it replaces.
func assembleFlags(req Request, facts serving.ArtifactFacts, profile serveprofiles.ArchProfile, quantFlags []string, gpuMemUtil float64, maxNumSeqs int, enforceEager bool) []string {
	wants := func(c serving.Capability) bool {
		return slices.Contains(req.Capabilities, c)
	}

	dtype := req.Dtype
	if dtype == "" {
		dtype = "auto"
	}

	flags := []string{
		"--model", facts.ModelPath,
		"--served-model-name", req.ServedName,
		"--dtype", dtype,
	}
	// Context length and memory utilization together size the KV cache — the
	// resource-budget group. Both are omitted when unset so vLLM keeps its own
	// defaults (the single-instance case uses the whole box).
	if req.ContextLen > 0 {
		flags = append(flags, "--max-model-len", fmt.Sprintf("%d", req.ContextLen))
	}
	if gpuMemUtil > 0 {
		flags = append(flags, "--gpu-memory-utilization", fmt.Sprintf("%g", gpuMemUtil))
	}
	if maxNumSeqs > 0 {
		flags = append(flags, "--max-num-seqs", fmt.Sprintf("%d", maxNumSeqs))
	}
	// Eager execution belongs with the resource group: it is what the
	// accelerator's runtime can survive, and it zeroes the graph-capture term
	// of the memory budget.
	if enforceEager {
		flags = append(flags, "--enforce-eager")
	}

	// Thinking → reasoning parser + enable_thinking. Without it, the reasoning
	// parser is omitted so guided decoding stays live (the AGENTS.md root-cause).
	thinking := wants(serving.Thinking)
	if thinking && profile.ReasoningParser != "" {
		flags = append(flags, "--reasoning-parser", profile.ReasoningParser)
	}
	flags = append(flags, "--default-chat-template-kwargs", chatTemplateKwargs(thinking))

	// Tool-calling → auto tool choice + the arch's tool parser.
	if wants(serving.ToolCalling) && profile.ToolCallParser != "" {
		flags = append(flags, "--enable-auto-tool-choice", "--tool-call-parser", profile.ToolCallParser)
	}

	// Quant flags (single authority).
	flags = append(flags, quantFlags...)

	// trust-remote-code when the artifact ships modeling modules.
	if facts.NeedsRemoteCode {
		flags = append(flags, "--trust-remote-code")
	}

	// Mistral/Tekken tokenizer selects mistral tokenizer-mode (guarded against
	// vision by the compat rule above).
	if facts.Tokenizer == serving.TokenizerMistral {
		flags = append(flags, "--tokenizer-mode", "mistral")
	}

	return flags
}

// chatTemplateKwargs renders the --default-chat-template-kwargs JSON for the
// enable_thinking toggle.
func chatTemplateKwargs(thinking bool) string {
	return fmt.Sprintf(`{"enable_thinking": %t}`, thinking)
}

// CanonicalCapabilities returns the request capabilities de-duplicated and in
// canonical order, so equivalent requests resolve to the same key/flags.
func CanonicalCapabilities(caps []serving.Capability) []serving.Capability {
	seen := make(map[serving.Capability]bool, len(caps))
	out := make([]serving.Capability, 0, len(caps))
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}
