package lifecycle

import (
	"context"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
)

// ServingStatus is the derived state of a desired instance against the runtime.
// It is computed every time from Inspect + the prober — never read from the
// manifest.
type ServingStatus string

const (
	// StatusServing: identity verified, required services running, health ok,
	// warmup against the served model succeeded.
	StatusServing ServingStatus = "serving"
	// StatusNotServing: the stack is absent, incomplete, unhealthy, or not yet
	// answering for the served model. Fail-closed default.
	StatusNotServing ServingStatus = "not-serving"
	// StatusConflict: a stack exists under this project but its labels do NOT match
	// the desired identity (stale container, hand-launched, reused project name).
	// Never adopted.
	StatusConflict ServingStatus = "conflict"
	// StatusUnknown: the runtime could not be inspected (engine unreachable). Fail
	// closed — treated as not-serving by every caller.
	StatusUnknown ServingStatus = "unknown"
)

// Reconciled is the outcome of a reconcile: the derived status and a human reason.
type Reconciled struct {
	Status ServingStatus
	Reason string
}

func (r Reconciled) serving() bool { return r.Status == StatusServing }

// requiredServices are the compose services that must be running for an instance
// to count as serving. The watchdog is required because wedge detection is a
// promised property (drive-the-driver choice): a stack without it is degraded,
// so reconcile fails closed.
var requiredServices = []string{"vllm", "watchdog"}

// Reconcile derives the serving status of a desired block against the runtime.
// The predicate (B1): runtime reachable → stack exists → every managed container
// matches the desired identity (else conflict) → all required services running
// (else not-serving; missing watchdog ⇒ fail closed) → /health 200 → warmup
// against the exact served name. Any failure short-circuits to a fail-closed
// status; only the full predicate yields serving.
func Reconcile(ctx context.Context, rt runtime.Runtime, pr runtime.Prober, d instance.Desired, endpoint string) Reconciled {
	state, err := rt.Inspect(ctx, d.ProjectName, d.SpecPath)
	if err != nil {
		return Reconciled{StatusUnknown, "runtime unreachable: " + err.Error()}
	}
	if !state.Exists {
		return Reconciled{StatusNotServing, "no stack for this instance"}
	}

	want := IdentityLabels(d)
	running := map[string]bool{}
	for _, svc := range state.Services {
		if !matchesIdentity(svc.Labels, want) {
			return Reconciled{StatusConflict, "a container under project " + d.ProjectName +
				" does not carry this instance's identity (stale or hand-launched stack); refusing to adopt"}
		}
		if svc.Running {
			running[svc.Labels[composeServiceLabel]] = true
		}
	}
	for _, req := range requiredServices {
		if !running[req] {
			return Reconciled{StatusNotServing, "required service not running: " + req +
				ifWatchdog(req, " (wedge detection absent — failing closed)")}
		}
	}

	if ok, err := pr.Health(ctx, endpoint); err != nil || !ok {
		return Reconciled{StatusNotServing, "health check not passing"}
	}
	if ok, err := pr.Warmup(ctx, endpoint, d.ServedName); err != nil || !ok {
		return Reconciled{StatusNotServing, "warmup against served model did not confirm serving"}
	}
	return Reconciled{StatusServing, "serving"}
}

func ifWatchdog(svc, suffix string) string {
	if svc == "watchdog" {
		return suffix
	}
	return ""
}
