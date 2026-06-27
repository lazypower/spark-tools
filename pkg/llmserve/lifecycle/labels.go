// Package lifecycle owns a managed serving instance's lifecycle by driving the
// emitted spec (B1 of llm-serve v2): bring up to CONFIRMED serving, keep
// desired==actual, tear down cleanly. It never reports a state it cannot verify —
// "serving" is a derived predicate over the runtime, never a stored flag — and it
// is fail-closed on every ambiguity.
package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
)

// Identity label keys. Every distinguishing field of the desired record is
// stamped as an immutable label on the emitted stack, so reconcile can prove a
// running container IS this instance (and not a stale or hand-launched stack
// reusing the project name). If any of these does not match, reconcile reports a
// conflict and refuses adoption.
const (
	LabelManagedBy   = "managed-by"
	LabelInstance    = "instance"
	LabelContractKey = "contract-key"
	LabelSpecHash    = "spec-hash"
	LabelServedName  = "served-name"
	LabelModelID     = "model-id"
	LabelModelRev    = "model-revision"
	LabelTarget      = "target-fingerprint"

	// ManagedByValue marks a stack as llm-serve-owned (the filter Inspect uses).
	ManagedByValue = "llm-serve"

	// composeServiceLabel is docker compose's per-service label, used to tell the
	// engine service from the watchdog sidecar in the required-service check.
	composeServiceLabel = "com.docker.compose.service"
)

// IdentityLabels is the canonical label set for a desired instance — the SINGLE
// definition used both to stamp the emitted stack (emit) and to verify it
// (reconcile), so the two can never drift.
func IdentityLabels(d instance.Desired) map[string]string {
	return map[string]string{
		LabelManagedBy:   ManagedByValue,
		LabelInstance:    d.Name,
		LabelContractKey: d.ContractKey.Canonical(),
		LabelSpecHash:    d.SpecHash,
		LabelServedName:  d.ServedName,
		LabelModelID:     d.ModelID,
		LabelModelRev:    d.ModelRevision,
		LabelTarget:      d.Target.Canonical(),
	}
}

// matchesIdentity reports whether a container's labels carry the full identity of
// the desired instance. Every identity key must be present and equal; a single
// mismatch means the running container is NOT this instance.
func matchesIdentity(containerLabels, want map[string]string) bool {
	for k, v := range want {
		if containerLabels[k] != v {
			return false
		}
	}
	return true
}

// SameIdentity reports whether two desired records are the SAME managed
// configuration across the FULL identity (not just the contract key — which omits
// model id/revision/served name/target). A re-up of the same identity is an
// idempotent no-op; any identity difference is a change that triggers a replace.
func SameIdentity(a, b instance.Desired) bool {
	return maps.Equal(IdentityLabels(a), IdentityLabels(b))
}

// IdentityTag is a stable short hash of the full identity, used to key the
// managed spec file so two DIFFERENT identities that happen to render the same
// command (same SpecHash) never share a spec path — which would let a replace's
// candidate overwrite the current spec and lose it for restore.
func IdentityTag(d instance.Desired) string {
	labels := IdentityLabels(d)
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k + "=" + labels[k] + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
