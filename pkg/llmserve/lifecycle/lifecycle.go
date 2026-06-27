package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
)

// Plan is a validated bring-up request: the desired record (identity + intent,
// minus the spec path the orchestrator owns) and the emitted launch spec content
// (already carrying the identity labels). The caller does resolve+emit; the
// orchestrator owns the transactional apply.
type Plan struct {
	Desired instance.Desired
	Spec    string
}

// Result is the outcome of an Up: the derived status, a reason, and whether it
// replaced a different existing contract.
type Result struct {
	Status   ServingStatus
	Reason   string
	Replaced bool
}

// Orchestrator drives instance lifecycles by applying emitted specs and
// reconciling against the runtime. Mutations and recovery hold the store's host
// lock; reads (Status/List) are pure.
type Orchestrator struct {
	Store        *instance.Store
	Runtime      runtime.Runtime
	Prober       runtime.Prober
	SpecDir      string        // where managed specs are written
	BootTimeout  time.Duration // max wait for the serving predicate
	PollInterval time.Duration // between predicate polls
}

func (o *Orchestrator) bootTimeout() time.Duration {
	if o.BootTimeout <= 0 {
		return 7 * time.Minute // boot is ~3–7 min on the Spark
	}
	return o.BootTimeout
}

func (o *Orchestrator) pollInterval() time.Duration {
	if o.PollInterval <= 0 {
		return 5 * time.Second
	}
	return o.PollInterval
}

// specPath is keyed by name AND the full IDENTITY tag, so a replace's candidate
// spec never overwrites the current spec. Keying on the command hash alone was
// unsafe: two distinct identities (e.g. differing only by target accelerator or
// model revision) can render the same command and would collide, letting the
// candidate clobber current and lose it for restore.
func (o *Orchestrator) specPath(d instance.Desired) string {
	return filepath.Join(o.SpecDir, d.Name+"-"+IdentityTag(d)+".compose.yml")
}

func (o *Orchestrator) writeSpec(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating spec dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}
	return nil
}

// Up brings an instance to confirmed serving. It is idempotent (re-up of an
// already-serving identical contract is a no-op), and a changed contract is a
// named destructive replace. It holds the host lock and runs in-flight recovery
// first.
func (o *Orchestrator) Up(ctx context.Context, plan Plan) (Result, error) {
	if !instance.ValidName(plan.Desired.Name) {
		return Result{}, fmt.Errorf("invalid instance name %q", plan.Desired.Name)
	}
	unlock, err := o.Store.Lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()

	o.recoverLocked(ctx)

	name := plan.Desired.Name
	existing, err := o.Store.Load(name)
	switch {
	case errors.Is(err, instance.ErrNotFound):
		return o.bringUp(ctx, plan)
	case err != nil:
		return Result{}, err
	default:
		// Idempotency is keyed on the FULL identity, not just the contract key
		// (which omits model id/revision/served name/target) — otherwise a
		// revision bump with an unchanged contract key would be reported "already
		// serving" the old model and never applied.
		if SameIdentity(existing.Desired, plan.Desired) {
			rec := Reconcile(ctx, o.Runtime, o.Prober, existing.Desired, existing.Desired.Endpoint)
			if rec.serving() {
				return Result{StatusServing, "already serving (no change)", false}, nil
			}
			return o.bringUp(ctx, plan) // same identity, not serving — (re)bring up
		}
		return o.replace(ctx, existing.Desired, plan)
	}
}

// bringUp is the fresh/re-up transaction: spec → manifest(starting) → compose up
// → wait for serving → clear, with fail-closed cleanup on any failure.
func (o *Orchestrator) bringUp(ctx context.Context, plan Plan) (Result, error) {
	d := plan.Desired
	d.SpecPath = o.specPath(d)
	if err := o.writeSpec(d.SpecPath, plan.Spec); err != nil {
		return Result{}, err
	}
	// Crash-safe ordering: spec exists, THEN manifest, THEN runtime resources.
	if err := o.Store.Save(instance.Instance{Desired: d, Operation: &instance.Operation{Phase: instance.PhaseStarting}}); err != nil {
		return Result{}, err
	}
	if err := o.Runtime.Up(ctx, d.ProjectName, d.SpecPath); err != nil {
		return o.failCleanup(ctx, d, "compose up failed: "+err.Error())
	}
	if rec := o.waitServing(ctx, d); !rec.serving() {
		return o.failCleanup(ctx, d, "did not reach serving: "+rec.Reason)
	}
	if err := o.Store.Save(instance.Instance{Desired: d}); err != nil { // clear operation
		return Result{}, err
	}
	return Result{StatusServing, "serving", false}, nil
}

// replace is the destructive current+candidate transaction (single port ⇒ no
// overlap). The candidate is validated offline already (the Plan exists); current
// is torn down, candidate brought up; on success candidate is promoted, on
// failure current is best-effort restored and never falsely reported serving.
func (o *Orchestrator) replace(ctx context.Context, current instance.Desired, plan Plan) (Result, error) {
	cand := plan.Desired
	cand.SpecPath = o.specPath(cand)
	if err := o.writeSpec(cand.SpecPath, plan.Spec); err != nil {
		return Result{}, err
	}
	// Record current + candidate before mutating, so a crash mid-replace is recoverable.
	if err := o.Store.Save(instance.Instance{
		Desired:   current,
		Candidate: &cand,
		Operation: &instance.Operation{Phase: instance.PhaseReplacing},
	}); err != nil {
		return Result{}, err
	}

	_ = o.Runtime.Down(ctx, current.ProjectName, current.SpecPath) // free the port
	if err := o.Runtime.Up(ctx, cand.ProjectName, cand.SpecPath); err == nil {
		if rec := o.waitServing(ctx, cand); rec.serving() {
			if err := o.Store.Save(instance.Instance{Desired: cand}); err != nil { // promote
				return Result{}, err
			}
			o.gcSpec(current, cand)
			return Result{StatusServing, "replaced and serving", true}, nil
		}
	}
	// Candidate failed — discard it and best-effort restore current.
	_ = o.Runtime.Down(ctx, cand.ProjectName, cand.SpecPath)
	return o.restoreCurrent(ctx, current)
}

// restoreCurrent attempts to bring the prior instance back after a failed replace.
// It NEVER clears to a normal desired=current state unless current's OWN serving
// predicate is re-confirmed; otherwise it leaves a cleanup_required recovery handle.
func (o *Orchestrator) restoreCurrent(ctx context.Context, current instance.Desired) (Result, error) {
	if err := o.Runtime.Up(ctx, current.ProjectName, current.SpecPath); err == nil {
		if rec := o.waitServing(ctx, current); rec.serving() {
			if err := o.Store.Save(instance.Instance{Desired: current}); err != nil {
				return Result{}, err
			}
			return Result{StatusServing, "replace failed; restored prior instance", false},
				fmt.Errorf("replacement failed; prior instance restored and serving")
		}
	}
	o.markCleanupRequired(current)
	return Result{StatusNotServing, "replace failed and prior instance could not be confirmed serving", false},
		fmt.Errorf("replacement failed; prior instance unconfirmed — recovery pending")
}

// failCleanup tears down a failed bring-up. It removes the manifest ONLY on
// CONFIRMED runtime absence; if teardown can't be confirmed it keeps a
// cleanup_required recovery handle (never erases the only handle to an orphan).
func (o *Orchestrator) failCleanup(ctx context.Context, d instance.Desired, reason string) (Result, error) {
	if o.confirmedDown(ctx, d) {
		_ = o.Store.Delete(d.Name)
		return Result{StatusNotServing, reason + " (cleaned up)", false}, errors.New(reason)
	}
	o.markCleanupRequired(d)
	return Result{StatusNotServing, reason + " (cleanup unconfirmed — kept as recovery handle)", false},
		fmt.Errorf("%s (cleanup_required)", reason)
}

// confirmedDown ensures no stack remains for an instance. Absence is proven by
// Inspect (which queries the runtime by label, independent of the spec file), so
// if nothing is running we confirm immediately — even when the spec is
// unparseable and `compose down` would fail (the never-started / broken-spec
// case). Only when a stack IS present do we require a clean teardown AND a
// follow-up Inspect showing it gone. Any Inspect error ⇒ unconfirmed (fail closed).
func (o *Orchestrator) confirmedDown(ctx context.Context, d instance.Desired) bool {
	if state, err := o.Runtime.Inspect(ctx, d.ProjectName, d.SpecPath); err == nil && !state.Exists {
		return true // nothing to tear down — absence already proven by the runtime
	}
	downErr := o.Runtime.Down(ctx, d.ProjectName, d.SpecPath)
	state, inspErr := o.Runtime.Inspect(ctx, d.ProjectName, d.SpecPath)
	return downErr == nil && inspErr == nil && !state.Exists
}

func (o *Orchestrator) markCleanupRequired(d instance.Desired) {
	_ = o.Store.Save(instance.Instance{Desired: d, Operation: &instance.Operation{Phase: instance.PhaseCleanupRequired}})
}

// gcSpec removes the superseded current spec file after a successful promote
// (best-effort; a leftover spec is harmless).
func (o *Orchestrator) gcSpec(old, current instance.Desired) {
	if old.SpecPath != "" && old.SpecPath != current.SpecPath {
		_ = os.Remove(old.SpecPath)
	}
}

// waitServing polls the serving predicate until it holds or the boot timeout
// elapses, returning the last reconcile.
func (o *Orchestrator) waitServing(ctx context.Context, d instance.Desired) Reconciled {
	deadline := time.Now().Add(o.bootTimeout())
	for {
		rec := Reconcile(ctx, o.Runtime, o.Prober, d, d.Endpoint)
		if rec.serving() {
			return rec
		}
		if time.Now().After(deadline) {
			return rec
		}
		select {
		case <-ctx.Done():
			return Reconciled{StatusUnknown, ctx.Err().Error()}
		case <-time.After(o.pollInterval()):
		}
	}
}

// Down tears an instance down. Confirmed teardown removes the manifest; an
// unconfirmed one keeps a cleanup_required handle. Downing an absent instance is
// a no-op.
func (o *Orchestrator) Down(ctx context.Context, name string) error {
	unlock, err := o.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	in, err := o.Store.Load(name)
	if errors.Is(err, instance.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = o.Store.Save(instance.Instance{Desired: in.Desired, Operation: &instance.Operation{Phase: instance.PhaseStopping}})
	if o.confirmedDown(ctx, in.Desired) {
		return o.Store.Delete(name)
	}
	o.markCleanupRequired(in.Desired)
	return fmt.Errorf("could not confirm teardown of %q; kept as recovery handle (run `recover`, or `forget --force`)", name)
}

// Forget abandons a manifest stuck in cleanup_required. It prefers confirmed
// absence; if absence can't be confirmed it abandons ONLY when acceptOrphan is
// set (the operator owning the risk of a possibly-orphaned stack).
func (o *Orchestrator) Forget(ctx context.Context, name string, acceptOrphan bool) error {
	unlock, err := o.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	in, err := o.Store.Load(name)
	if errors.Is(err, instance.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.confirmedDown(ctx, in.Desired) {
		return o.Store.Delete(name)
	}
	if acceptOrphan {
		return o.Store.Delete(name)
	}
	return fmt.Errorf("cannot confirm %q's runtime is gone; re-run with accept-orphan to abandon a possibly-running stack", name)
}

// Recover resolves all in-flight manifests against the runtime under the lock.
func (o *Orchestrator) Recover(ctx context.Context) error {
	unlock, err := o.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	o.recoverLocked(ctx)
	return nil
}

// recoverLocked resolves every in-flight manifest (caller holds the lock). It
// finishes or rolls back half-done mutations; steady manifests are left to the
// pure Status read to report (drift is reported, not silently auto-relaunched by
// an unrelated command).
func (o *Orchestrator) recoverLocked(ctx context.Context) {
	list, err := o.Store.List()
	if err != nil {
		return
	}
	for i := range list {
		in := list[i]
		if !in.InFlight() {
			continue
		}
		switch in.Operation.Phase {
		case instance.PhaseStarting:
			if Reconcile(ctx, o.Runtime, o.Prober, in.Desired, in.Desired.Endpoint).serving() {
				_ = o.Store.Save(instance.Instance{Desired: in.Desired}) // adopt
			} else {
				o.failCleanupSilent(ctx, in.Desired)
			}
		case instance.PhaseReplacing:
			if in.Candidate != nil && Reconcile(ctx, o.Runtime, o.Prober, *in.Candidate, in.Candidate.Endpoint).serving() {
				_ = o.Store.Save(instance.Instance{Desired: *in.Candidate}) // promote
			} else {
				_, _ = o.restoreCurrent(ctx, in.Desired)
			}
		case instance.PhaseStopping, instance.PhaseCleanupRequired:
			if o.confirmedDown(ctx, in.Desired) {
				_ = o.Store.Delete(in.Desired.Name)
			} else {
				o.markCleanupRequired(in.Desired)
			}
		}
	}
}

func (o *Orchestrator) failCleanupSilent(ctx context.Context, d instance.Desired) {
	if o.confirmedDown(ctx, d) {
		_ = o.Store.Delete(d.Name)
		return
	}
	o.markCleanupRequired(d)
}

// InstanceStatus pairs a manifest with its reconciled runtime status.
type InstanceStatus struct {
	Instance instance.Instance
	Reconciled
}

// Status reconciles one instance (pure read — no lock, no mutation). A desired
// record with no running stack reports not-serving/recovery-pending, never
// "stopped" (stopped is the absence of a manifest).
func (o *Orchestrator) Status(ctx context.Context, name string) (InstanceStatus, error) {
	in, err := o.Store.Load(name)
	if err != nil {
		return InstanceStatus{}, err
	}
	return InstanceStatus{*in, Reconcile(ctx, o.Runtime, o.Prober, in.Desired, in.Desired.Endpoint)}, nil
}

// List reconciles all managed instances (pure read).
func (o *Orchestrator) List(ctx context.Context) ([]InstanceStatus, error) {
	manifests, err := o.Store.List()
	if err != nil {
		return nil, err
	}
	out := make([]InstanceStatus, 0, len(manifests))
	for i := range manifests {
		in := manifests[i]
		out = append(out, InstanceStatus{in, Reconcile(ctx, o.Runtime, o.Prober, in.Desired, in.Desired.Endpoint)})
	}
	return out, nil
}
