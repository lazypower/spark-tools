package lifecycle

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// fakeRuntime tracks which spec is currently "up" and returns the RuntimeState
// the test mapped for that spec. Down clears the active spec when confirmsDown.
type fakeRuntime struct {
	serveFor     map[string]runtime.RuntimeState
	active       string
	upErr        error
	downErr      error
	inspErr      error
	confirmsDown bool
	ups, downs   int
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{serveFor: map[string]runtime.RuntimeState{}, confirmsDown: true}
}

func (f *fakeRuntime) Up(_ context.Context, _, spec string) error {
	f.ups++
	if f.upErr != nil {
		return f.upErr
	}
	f.active = spec
	return nil
}

func (f *fakeRuntime) Down(_ context.Context, _, _ string) error {
	f.downs++
	if f.downErr != nil {
		return f.downErr
	}
	if f.confirmsDown {
		f.active = ""
	}
	return nil
}

func (f *fakeRuntime) Inspect(_ context.Context, _, _ string) (runtime.RuntimeState, error) {
	if f.inspErr != nil {
		return runtime.RuntimeState{}, f.inspErr
	}
	if f.active == "" {
		return runtime.RuntimeState{}, nil
	}
	return f.serveFor[f.active], nil
}

func (f *fakeRuntime) ListManaged(context.Context) ([]runtime.ServiceState, error) {
	if f.inspErr != nil {
		return nil, f.inspErr
	}
	if f.active == "" {
		return nil, nil
	}
	return f.serveFor[f.active].Services, nil
}

type fakeProber struct{ health, warmup bool }

func (p fakeProber) Health(context.Context, string) (bool, error) { return p.health, nil }
func (p fakeProber) Warmup(context.Context, string, string) (bool, error) {
	return p.warmup, nil
}

func desired(name, hash, key string) instance.Desired {
	return instance.Desired{
		Name:        name,
		ServedName:  name,
		ModelID:     "org/" + name,
		SpecHash:    hash,
		ContractKey: serving.ContractKey{Arch: "Qwen3MoeForCausalLM", Mode: key},
		ProjectName: "llm-serve-" + name,
		Endpoint:    "http://localhost:8000",
	}
}

// servingState builds a RuntimeState whose containers carry the desired identity
// and the required services, so Reconcile (with a healthy prober) yields serving.
func servingState(d instance.Desired) runtime.RuntimeState {
	want := IdentityLabels(d)
	mk := func(svc string) runtime.ServiceState {
		l := maps.Clone(want)
		l[composeServiceLabel] = svc
		return runtime.ServiceState{Name: svc, Running: true, Labels: l}
	}
	return runtime.RuntimeState{Exists: true, Services: []runtime.ServiceState{mk("vllm"), mk("watchdog")}}
}

func newOrch(t *testing.T, rt runtime.Runtime, pr runtime.Prober) *Orchestrator {
	t.Helper()
	return &Orchestrator{
		Store:        instance.NewStore(t.TempDir()),
		Runtime:      rt,
		Prober:       pr,
		SpecDir:      t.TempDir(),
		BootTimeout:  20 * time.Millisecond,
		PollInterval: 2 * time.Millisecond,
	}
}

func TestUp_FreshSuccess(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)

	res, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if res.Status != StatusServing {
		t.Fatalf("status = %q, want serving", res.Status)
	}
	in, err := o.Store.Load("qwen")
	if err != nil {
		t.Fatalf("manifest must persist after success: %v", err)
	}
	if in.Operation != nil {
		t.Errorf("operation must be cleared on success, got %v", in.Operation.Phase)
	}
}

func TestUp_FailsToServe_ConfirmedCleanup(t *testing.T) {
	rt := newFakeRuntime()                                       // confirmsDown=true
	o := newOrch(t, rt, fakeProber{health: true, warmup: false}) // warmup never passes
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)

	_, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"})
	if err == nil {
		t.Fatal("a never-serving bring-up must fail")
	}
	if _, err := o.Store.Load("qwen"); err != instance.ErrNotFound {
		t.Error("confirmed cleanup must remove the manifest (no orphan record)")
	}
	if rt.downs == 0 {
		t.Error("cleanup must tear the stack down")
	}
}

func TestUp_FailsToServe_UnconfirmedCleanup_KeepsHandle(t *testing.T) {
	rt := newFakeRuntime()
	rt.confirmsDown = false // Down can't confirm absence
	o := newOrch(t, rt, fakeProber{health: true, warmup: false})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)

	_, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"})
	if err == nil {
		t.Fatal("expected failure")
	}
	in, err := o.Store.Load("qwen")
	if err != nil {
		t.Fatal("unconfirmed cleanup must KEEP the manifest as a recovery handle")
	}
	if in.Operation == nil || in.Operation.Phase != instance.PhaseCleanupRequired {
		t.Errorf("expected cleanup_required, got %+v", in.Operation)
	}
}

func TestUp_FailsFast_OnEngineCrashLoop(t *testing.T) {
	// Operator P1 follow-up: a crash-LOOPING engine (restart:unless-stopped keeps
	// it "restarting", never "exited") must fail fast via the restart count, NOT
	// wait out the boot ceiling.
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: false, warmup: false})
	o.BootTimeout = time.Hour // a ceiling we must NOT wait out
	d := desired("qwen", "hashA", "base")
	// The engine is crash-looping: high restart count (momentarily up between restarts).
	want := IdentityLabels(d)
	crashed := maps.Clone(want)
	crashed[composeServiceLabel] = "vllm"
	rt.serveFor[o.specPath(d)] = runtime.RuntimeState{
		Exists: true, Services: []runtime.ServiceState{{Name: "vllm", Running: true, RestartCount: 23, Labels: crashed}},
	}

	done := make(chan struct{})
	var rerr error
	go func() {
		_, rerr = o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("up must fail fast on a crashed engine, not wait the boot ceiling")
	}
	if rerr == nil {
		t.Error("a crashed engine must fail the bring-up")
	}
}

func TestUp_Idempotent_AlreadyServing(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)

	if _, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	upsAfterFirst := rt.ups
	res, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusServing {
		t.Errorf("re-up of serving instance should report serving, got %q", res.Status)
	}
	if rt.ups != upsAfterFirst {
		t.Errorf("idempotent re-up must NOT launch again (ups %d→%d)", upsAfterFirst, rt.ups)
	}
}

func TestUp_DestructiveReplace_Success(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	a := desired("qwen", "hashA", "base")
	b := desired("qwen", "hashB", "thinking") // different contract key
	rt.serveFor[o.specPath(a)] = servingState(a)
	rt.serveFor[o.specPath(b)] = servingState(b)

	if _, err := o.Up(context.Background(), Plan{Desired: a, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	res, err := o.Up(context.Background(), Plan{Desired: b, Spec: "spec-B"})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if !res.Replaced || res.Status != StatusServing {
		t.Errorf("expected replaced+serving, got %+v", res)
	}
	in, _ := o.Store.Load("qwen")
	if in.Desired.ContractKey.Mode != "thinking" || in.Candidate != nil || in.Operation != nil {
		t.Errorf("after promote: desired=B, no candidate, no operation; got %+v", in)
	}
}

func TestUp_DestructiveReplace_FailRestoresCurrent(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	a := desired("qwen", "hashA", "base")
	b := desired("qwen", "hashB", "thinking")
	rt.serveFor[o.specPath(a)] = servingState(a)
	// candidate B is mapped to nothing → never serves.

	if _, err := o.Up(context.Background(), Plan{Desired: a, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	_, err := o.Up(context.Background(), Plan{Desired: b, Spec: "spec-B"})
	if err == nil {
		t.Fatal("a failing replace must return an error, not pretend the candidate serves")
	}
	in, err := o.Store.Load("qwen")
	if err != nil {
		t.Fatal("instance must survive a failed replace")
	}
	if in.Desired.ContractKey.Mode != "base" {
		t.Errorf("failed replace must restore current (base), got %q", in.Desired.ContractKey.Mode)
	}
	if in.Candidate != nil {
		t.Error("candidate must be discarded after a failed replace")
	}
}

func TestUp_RevisionChange_SameContractKey_Replaces(t *testing.T) {
	// codex Mode-B P1: a revision bump with an UNCHANGED contract key must replace,
	// not be reported "already serving" the old model.
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	a := desired("qwen", "hashA", "base")
	b := desired("qwen", "hashA", "base") // identical contract key
	b.ModelRevision = "rev2"              // but a different artifact revision
	rt.serveFor[o.specPath(a)] = servingState(a)
	rt.serveFor[o.specPath(b)] = servingState(b)

	if _, err := o.Up(context.Background(), Plan{Desired: a, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	res, err := o.Up(context.Background(), Plan{Desired: b, Spec: "spec-B"})
	if err != nil {
		t.Fatalf("revision change up: %v", err)
	}
	if !res.Replaced {
		t.Error("a revision change must trigger a replace, not a no-op")
	}
	in, _ := o.Store.Load("qwen")
	if in.Desired.ModelRevision != "rev2" {
		t.Errorf("revision change must be applied, desired still at %q", in.Desired.ModelRevision)
	}
}

func TestSpecPath_DistinctForSameCommandDifferentIdentity(t *testing.T) {
	// codex Mode-B P1: distinct identities that render the same command must NOT
	// share a spec path (else a replace's candidate clobbers current).
	o := newOrch(t, newFakeRuntime(), fakeProber{})
	a := desired("qwen", "samehash", "base")
	b := desired("qwen", "samehash", "base")
	b.Target.Accelerator = "other-accel" // same SpecHash, different identity
	if o.specPath(a) == o.specPath(b) {
		t.Error("distinct identities must get distinct spec paths even with an identical command hash")
	}
}

func TestDown_Confirmed_RemovesManifest(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)
	if _, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	if err := o.Down(context.Background(), "qwen"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := o.Store.Load("qwen"); err != instance.ErrNotFound {
		t.Error("confirmed down must remove the manifest")
	}
}

func TestDown_Unconfirmed_KeepsCleanupRequired(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)
	if _, err := o.Up(context.Background(), Plan{Desired: d, Spec: "spec-A"}); err != nil {
		t.Fatal(err)
	}
	rt.confirmsDown = false // teardown now cannot be confirmed
	if err := o.Down(context.Background(), "qwen"); err == nil {
		t.Fatal("unconfirmed down must report an error")
	}
	in, err := o.Store.Load("qwen")
	if err != nil || in.Operation == nil || in.Operation.Phase != instance.PhaseCleanupRequired {
		t.Errorf("unconfirmed down must keep a cleanup_required handle, got %+v err=%v", in, err)
	}
}

func TestForget_NeverStarted_ConfirmsWithoutAcceptOrphan(t *testing.T) {
	// Operator minor: a cleanup_required manifest whose stack never actually
	// started (nothing running) must be forgettable WITHOUT --accept-orphan —
	// Inspect proves absence even if `compose down` can't parse a broken spec.
	rt := newFakeRuntime()                // nothing active ⇒ Inspect reports !Exists
	rt.downErr = context.DeadlineExceeded // even if Down errors on a broken spec
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	if err := o.Store.Save(instance.Instance{Desired: d, Operation: &instance.Operation{Phase: instance.PhaseCleanupRequired}}); err != nil {
		t.Fatal(err)
	}
	if err := o.Forget(context.Background(), "qwen", false); err != nil {
		t.Errorf("a never-started cleanup_required must forget without --accept-orphan, got %v", err)
	}
	if _, err := o.Store.Load("qwen"); err != instance.ErrNotFound {
		t.Error("manifest should be cleared")
	}
}

func TestDown_Absent_NoOp(t *testing.T) {
	o := newOrch(t, newFakeRuntime(), fakeProber{})
	if err := o.Down(context.Background(), "ghost"); err != nil {
		t.Errorf("downing an absent instance must be a no-op, got %v", err)
	}
}

func TestStatus_Conflict_OnLabelMismatch(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	// A stack exists under the project but with foreign labels (hand-launched).
	foreign := runtime.RuntimeState{Exists: true, Services: []runtime.ServiceState{
		{Name: "vllm", Running: true, Labels: map[string]string{"managed-by": "someone-else"}},
	}}
	rt.serveFor[o.specPath(d)] = foreign
	rt.active = o.specPath(d)
	if err := o.Store.Save(instance.Instance{Desired: d}); err != nil {
		t.Fatal(err)
	}
	st, err := o.Status(context.Background(), "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusConflict {
		t.Errorf("a foreign-labeled stack must be a conflict, got %q", st.Status)
	}
}

func TestStatus_DriftNotStopped(t *testing.T) {
	// A desired with no running stack is drift (not-serving), never "stopped".
	rt := newFakeRuntime() // nothing active
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	if err := o.Store.Save(instance.Instance{Desired: d}); err != nil {
		t.Fatal(err)
	}
	st, err := o.Status(context.Background(), "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusNotServing {
		t.Errorf("desired-with-no-stack must be not-serving (drift), got %q", st.Status)
	}
}

func TestReconcile_UnreachableRuntime_FailsClosed(t *testing.T) {
	rt := newFakeRuntime()
	rt.inspErr = context.DeadlineExceeded
	d := desired("qwen", "hashA", "base")
	rec := Reconcile(context.Background(), rt, fakeProber{}, d, d.Endpoint)
	if rec.Status != StatusUnknown {
		t.Errorf("unreachable runtime must be unknown (fail closed), got %q", rec.Status)
	}
}

func TestReconcile_MissingWatchdog_FailsClosed(t *testing.T) {
	rt := newFakeRuntime()
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	// Only the engine is running; the watchdog is absent.
	want := IdentityLabels(d)
	engine := want
	engine[composeServiceLabel] = "vllm"
	rt.serveFor[o.specPath(d)] = runtime.RuntimeState{
		Exists: true, Services: []runtime.ServiceState{{Name: "vllm", Running: true, Labels: engine}},
	}
	rt.active = o.specPath(d)
	rec := Reconcile(context.Background(), rt, fakeProber{health: true, warmup: true}, d, d.Endpoint)
	if rec.Status != StatusNotServing {
		t.Errorf("missing watchdog must fail closed (not-serving), got %q: %s", rec.Status, rec.Reason)
	}
}

func TestForget_RefusesWhenStackMayLive(t *testing.T) {
	rt := newFakeRuntime()
	rt.confirmsDown = false
	o := newOrch(t, rt, fakeProber{health: true, warmup: true})
	d := desired("qwen", "hashA", "base")
	rt.serveFor[o.specPath(d)] = servingState(d)
	rt.active = o.specPath(d)
	_ = o.Store.Save(instance.Instance{Desired: d, Operation: &instance.Operation{Phase: instance.PhaseCleanupRequired}})

	if err := o.Forget(context.Background(), "qwen", false); err == nil {
		t.Error("forget without accept-orphan must refuse when absence can't be confirmed")
	}
	if _, err := o.Store.Load("qwen"); err != nil {
		t.Error("refused forget must leave the manifest intact")
	}
	if err := o.Forget(context.Background(), "qwen", true); err != nil {
		t.Errorf("forget --accept-orphan must abandon, got %v", err)
	}
	if _, err := o.Store.Load("qwen"); err != instance.ErrNotFound {
		t.Error("accepted forget must remove the manifest")
	}
}
