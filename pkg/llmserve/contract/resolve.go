// Package contract resolves a serve request against the arch profiles into a
// validated vLLM launch spec: it realizes requested capabilities as backend
// flags, rejects incompatible combinations (the negative-compat rules), and
// stamps the contract key. This is the (A) contract engine — the value-density
// slice. It stops at producing a validated, ordered flag set; rendering that to
// a compose/docker-run/quadlet spec is the emit driver's job, and launching it
// is v2 (B). Resolution is a pure function of (request, artifact facts), so it
// is fully unit-testable off the GPU.
package contract

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmserve/fingerprint"
	"github.com/lazypower/spark-tools/pkg/llmserve/profiles"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
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
	profile, ok := profiles.Lookup(facts.Arch)
	if !ok {
		return nil, &RejectionError{
			Rule:   "unknown-arch",
			Reason: fmt.Sprintf("no serving profile for architecture %q", facts.Arch),
			Remedy: "add an arch profile entry, or serve a supported architecture",
		}
	}

	// 2. Quant method must be known — an unknown method might silently load wrong.
	quantFlags, ok := profiles.QuantFlagsFor(facts.Quant)
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
	creq := profiles.CompatRequest{Capabilities: req.Capabilities, Facts: facts, Profile: profile}
	for _, rule := range profiles.CompatRules {
		if bad, reason := rule.Violated(creq); bad {
			return nil, &RejectionError{Rule: rule.Name, Reason: reason, Remedy: rule.Remedy}
		}
	}

	// 5. Realize flags.
	flags := assembleFlags(req, facts, profile, quantFlags)

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

	return &Resolved{Key: key, Flags: flags, Warnings: warnings}, nil
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

// assembleFlags builds the ordered vLLM flag list from the validated request.
// Order mirrors the working oracle's compose command so an emitted spec reads
// like the hand-rolled one it replaces.
func assembleFlags(req Request, facts serving.ArtifactFacts, profile profiles.ArchProfile, quantFlags []string) []string {
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
	if req.ContextLen > 0 {
		flags = append(flags, "--max-model-len", fmt.Sprintf("%d", req.ContextLen))
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
