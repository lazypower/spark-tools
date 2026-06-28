// Package runtime is the drive-the-driver boundary: it brings an emitted launch
// spec up, tears it down, and inspects what is ACTUALLY running — without
// reimplementing the container runtime. The Runtime and Prober interfaces are the
// seam the lifecycle layer reconciles against; their real implementations exec
// `docker compose` and hit the vLLM HTTP endpoint, and are verified on the host
// (the Spark), while the lifecycle logic is unit-tested against fakes.
//
// Truth lives here, not in the manifest: lifecycle derives "is it serving" from
// Inspect + Prober every time, never from stored state.
package runtime

import "context"

// ServiceState is one container observed in the runtime, with the identity labels
// it was launched with (so reconcile can prove it IS the desired instance).
type ServiceState struct {
	// Name is the container/service name.
	Name string
	// Running is true when the container is up (not exited/created/dead).
	Running bool
	// RestartCount is how many times the runtime has restarted this container. A
	// container crash-looping under `restart: unless-stopped` is never "exited"
	// (it keeps coming back), so a climbing count — not a not-running snapshot — is
	// the reliable crash signal during a bring-up.
	RestartCount int
	// Labels are the container's labels (the identity stamp emit applied).
	Labels map[string]string
	// Mounts are the container's bind-mount HOST sources. B2 liveness reads these
	// to protect artifacts used by FOREIGN (non-llm-serve) containers — run.sh,
	// Ollama, hand-launched — which carry no llm-serve labels.
	Mounts []string
}

// RuntimeState is the observed state of a managed project's stack. Exists is true
// when any container for the project is present (running or not); Services lists
// them. An empty RuntimeState means the runtime has no trace of the project.
type RuntimeState struct {
	Exists   bool
	Services []ServiceState
}

// Runtime drives the host container runtime for one managed project. specPath is
// the emitted launch spec (compose file); projectName isolates the stack. All
// operations are addressed by (projectName, specPath) so a teardown/inspect can
// always find the exact stack a bring-up created.
type Runtime interface {
	// Up applies the spec and starts the stack detached. It does not wait for
	// readiness — that is the Prober's job, polled by lifecycle.
	Up(ctx context.Context, projectName, specPath string) error
	// Down stops and removes the stack. It must be idempotent (downing an absent
	// stack is not an error). A Down that cannot CONFIRM the stack is gone must
	// return an error so lifecycle keeps the recovery handle (cleanup_required).
	Down(ctx context.Context, projectName, specPath string) error
	// Inspect reports the actual runtime state of the project's stack.
	Inspect(ctx context.Context, projectName, specPath string) (RuntimeState, error)
	// ListRunning reports every RUNNING container on the host (not just
	// llm-serve-managed), with its labels and bind-mount sources. B2's liveness
	// query reads this so eviction protection is REALITY-based: a model served by
	// a foreign container (run.sh, Ollama, hand-launched) is still protected.
	ListRunning(ctx context.Context) ([]ServiceState, error)
}

// Prober checks a running endpoint. Both checks are part of the serving predicate;
// neither is ever cached into the manifest.
type Prober interface {
	// Health reports whether baseURL's /health returns 200.
	Health(ctx context.Context, baseURL string) (bool, error)
	// Warmup sends a minimal completion addressed to servedName and reports ok iff
	// a non-empty generation returns with no API error — the minimum evidence the
	// endpoint is actually serving THIS model (not B4 conformance).
	Warmup(ctx context.Context, baseURL, servedName string) (bool, error)
}
