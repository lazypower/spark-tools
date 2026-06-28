// Package serveinstance is the authority over a managed serving instance's on-disk
// state — slice B1 of llm-serve v2. It records ONE atomic manifest per instance:
// the DESIRED intent (and stable identity), an optional CANDIDATE block during a
// destructive replace, and the in-flight OPERATION phase needed to recover a
// half-done mutation after a crash.
//
// The manifest deliberately holds NO readiness or liveness: "is it serving" is
// ALWAYS derived from the runtime, never read back from here (design B1, the
// no-competing-authority invariant). The struct simply has nowhere to store it.
//
// Writes are atomic (temp + rename of the single file), so the desired/candidate/
// operation sections can never disagree across a crash. Mutations and recovery
// hold a host lock (Store.Lock); reads are pure snapshots.
package serveinstance

import (
	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serving"
)

// Desired is the pure intent + stable identity of a managed instance: what should
// be running, and everything that distinguishes this instance from another. Every
// distinguishing field here is also stamped as an immutable runtime label so
// reconcile can prove the running stack IS this instance (B1 identity rule).
type Desired struct {
	// Name is THE canonical identity; the project name, labels, and manifest
	// filename all derive from it.
	Name string `json:"name"`
	// ServedName is the vLLM --served-model-name alias.
	ServedName string `json:"served_name"`
	// ModelID / ModelRevision identify the verified artifact (immutable revision).
	ModelID       string `json:"model_id"`
	ModelRevision string `json:"model_revision"`
	// ModelDir is the host directory the artifact lives in. Covered derivatively by
	// SpecHash (it determines the emitted --model container path).
	ModelDir string `json:"model_dir"`
	// ContractKey is the serving-relevant tuple the launch was validated against.
	ContractKey serving.ContractKey `json:"contract_key"`
	// SpecPath is the managed path the emitted launch spec was written to (local
	// bookkeeping — not a runtime-identity label).
	SpecPath string `json:"spec_path"`
	// SpecHash is the content hash of the emitted spec (a runtime-identity label).
	SpecHash string `json:"spec_hash"`
	// Target is the engine+accelerator the launch was emitted for (identity labels).
	Target fingerprint.Fingerprint `json:"target"`
	// ProjectName is the compose/quadlet project name, derived deterministically
	// from Name.
	ProjectName string `json:"project_name"`
	// Endpoint is the base URL the instance serves on (e.g. http://localhost:8000),
	// where the serving predicate's health + warmup checks are sent. Not an
	// identity label (two instances can't share a port).
	Endpoint string `json:"endpoint"`
}

// Phase is the in-flight operation a manifest is mid-way through. Only
// non-terminal phases are stored — terminal states are NOT: "ready" is derived
// from the runtime, and "stopped" is the ABSENCE of a manifest. A manifest with
// no Operation and no running stack is DRIFT (runtime-absent), never "stopped".
type Phase string

const (
	// PhaseStarting: a bring-up is in progress (or crashed mid-bring-up).
	PhaseStarting Phase = "starting"
	// PhaseReplacing: a destructive replace is in progress; Candidate is populated.
	PhaseReplacing Phase = "replacing"
	// PhaseStopping: a teardown is in progress.
	PhaseStopping Phase = "stopping"
	// PhaseCleanupRequired: teardown could not confirm the stack is gone; the
	// manifest is kept as a recovery handle (never erased on unconfirmed cleanup).
	PhaseCleanupRequired Phase = "cleanup_required"
)

// Valid reports whether p is a known non-terminal phase.
func (p Phase) Valid() bool {
	switch p {
	case PhaseStarting, PhaseReplacing, PhaseStopping, PhaseCleanupRequired:
		return true
	default:
		return false
	}
}

// Operation is the recovery state of an in-flight mutation. nil Operation on an
// Instance means no mutation is in flight (a steady desired record).
type Operation struct {
	Phase Phase `json:"phase"`
}

// Instance is the one atomic manifest for a managed serving instance. Candidate
// is populated only during a replace (the block being brought up, promoted to
// Desired on success, discarded on failure). Operation is non-nil only while a
// mutation is in flight. There is intentionally no readiness/liveness field.
type Instance struct {
	Desired   Desired    `json:"desired"`
	Candidate *Desired   `json:"candidate,omitempty"`
	Operation *Operation `json:"operation,omitempty"`
}

// InFlight reports whether a mutation is mid-way (a non-nil operation phase),
// which is what startup recovery looks for — though recovery reconciles ALL
// manifests against the runtime, not only in-flight ones.
func (i *Instance) InFlight() bool {
	return i.Operation != nil && i.Operation.Phase.Valid()
}
