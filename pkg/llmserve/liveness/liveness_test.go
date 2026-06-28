package liveness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
)

// fakeRuntime serves a fixed set of running containers to ListRunning.
type fakeRuntime struct {
	managed []runtime.ServiceState // misnomer kept for diff size: the running set
	listErr error
}

func (f *fakeRuntime) Up(context.Context, string, string) error   { return nil }
func (f *fakeRuntime) Down(context.Context, string, string) error { return nil }
func (f *fakeRuntime) Inspect(context.Context, string, string) (runtime.RuntimeState, error) {
	return runtime.RuntimeState{}, nil
}
func (f *fakeRuntime) ListRunning(context.Context) ([]runtime.ServiceState, error) {
	return f.managed, f.listErr
}

// foreignContainer is a NON-llm-serve container (run.sh/Ollama/hand-launched):
// protected by its bind mounts, since we can't know which subdir it serves.
func foreignContainer(name string, mounts ...string) runtime.ServiceState {
	return runtime.ServiceState{Name: name, Running: true, Mounts: mounts}
}

// modelDir creates a real artifact directory (canonicalization resolves real
// paths) and returns its path.
func modelDir(t *testing.T, name string) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	return d
}

func runningContainer(name, hostPath string) runtime.ServiceState {
	return runtime.ServiceState{Name: name, Running: true, Labels: map[string]string{
		lifecycle.LabelManagedBy:        lifecycle.ManagedByValue,
		lifecycle.LabelInstance:         name,
		lifecycle.LabelArtifactHostPath: hostPath,
	}}
}

func storeWith(t *testing.T, dirs map[string]string) *instance.Store {
	t.Helper()
	s := instance.NewStore(t.TempDir())
	for name, dir := range dirs {
		if err := s.Save(instance.Instance{Desired: instance.Desired{Name: name, ModelDir: dir}}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestProtected_ByManifestIntent(t *testing.T) {
	qwen := modelDir(t, "Qwen")
	l := New(storeWith(t, map[string]string{"qwen": qwen}), &fakeRuntime{})
	if !l.IsProtected(context.Background(), qwen) {
		t.Error("a manifest's model dir must be protected (intent)")
	}
	if l.IsProtected(context.Background(), modelDir(t, "Other")) {
		t.Error("an unreferenced artifact must be evictable")
	}
}

func TestProtected_ByRunningOrphanContainer(t *testing.T) {
	qwen := modelDir(t, "Qwen")
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{runningContainer("qwen", qwen)}})
	if !l.IsProtected(context.Background(), qwen) {
		t.Error("a running managed container must protect its artifact even without a manifest")
	}
}

func TestEvictable_WhenNeitherRunningNorDesired(t *testing.T) {
	l := New(storeWith(t, nil), &fakeRuntime{})
	if l.IsProtected(context.Background(), modelDir(t, "Gone")) {
		t.Error("an artifact with no manifest and no running container must be evictable")
	}
}

func TestFailClosed_RuntimeUnreachable(t *testing.T) {
	l := New(storeWith(t, nil), &fakeRuntime{listErr: errors.New("docker down")})
	if !l.IsProtected(context.Background(), modelDir(t, "x")) {
		t.Error("docker unreachable must fail closed (everything protected)")
	}
	if _, all := l.ProtectedArtifacts(context.Background()); !all {
		t.Error("ProtectedArtifacts must report allProtected on a runtime error")
	}
}

func TestFailClosed_ManagedContainerMissingArtifactLabel(t *testing.T) {
	bad := runtime.ServiceState{Name: "mystery", Running: true, Labels: map[string]string{
		lifecycle.LabelManagedBy: lifecycle.ManagedByValue,
		lifecycle.LabelInstance:  "mystery",
	}}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{bad}})
	if !l.IsProtected(context.Background(), modelDir(t, "Anything")) {
		t.Error("an unmappable managed container must fail closed")
	}
}

func TestFailClosed_UnresolvableRecordedPath(t *testing.T) {
	// codex Mode-B P1: a running container whose recorded artifact path no longer
	// resolves (symlink removed post-launch) must fail closed, NOT silently
	// fall back to a mis-keyed comparison.
	gone := filepath.Join(t.TempDir(), "removed-symlink-target")
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{runningContainer("qwen", gone)}})
	if !l.IsProtected(context.Background(), modelDir(t, "RealOther")) {
		t.Error("an unresolvable recorded artifact path must fail closed (everything protected)")
	}
	if _, all := l.ProtectedArtifacts(context.Background()); !all {
		t.Error("an unresolvable recorded path must yield allProtected")
	}
}

func TestProtected_ForeignContainerMount_CoexistenceGuard(t *testing.T) {
	// The run.sh case: a foreign container bind-mounts the whole models dir. Every
	// artifact UNDER that mount is protected (broad but safe), so B3 won't prune a
	// live run.sh-served model.
	models := t.TempDir()
	served := filepath.Join(models, "Coder")
	if err := os.MkdirAll(served, 0755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(models, "AnotherModel")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{
		foreignContainer("vllm-runsh", models), // mounts the whole models store
	}})
	// Every model under the unlabeled mount is LOCKED — no introspection of which
	// one is loaded.
	for _, p := range []string{served, other, models} {
		if !l.IsProtected(context.Background(), p) {
			t.Errorf("%s under an unlabeled container's models mount must be locked", p)
		}
	}
	// And the unmanaged container is surfaced so llm-tidy can complain.
	report := l.Protected(context.Background())
	if len(report.Unmanaged) != 1 || report.Unmanaged[0].Container != "vllm-runsh" {
		t.Errorf("an unlabeled mounted container must be reported as unmanaged, got %+v", report.Unmanaged)
	}
}

func TestProtected_ManagedIsPrecise_B3CanStillPrune(t *testing.T) {
	// A MANAGED instance mounts the whole models dir but protects only its PRECISE
	// artifact — so B3 can prune OTHER unused models post-migration.
	models := t.TempDir()
	served := filepath.Join(models, "Served")
	unused := filepath.Join(models, "Unused")
	for _, d := range []string{served, unused} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{
		runningContainer("served", served), // managed: precise host-path label
	}})
	if !l.IsProtected(context.Background(), served) {
		t.Error("the managed instance's artifact must be protected")
	}
	if l.IsProtected(context.Background(), unused) {
		t.Error("an UNUSED model under the same mount must be prunable (managed protection is precise)")
	}
}

func TestProtected_OverlapBothDirections(t *testing.T) {
	models := t.TempDir()
	served := filepath.Join(models, "Coder")
	if err := os.MkdirAll(served, 0755); err != nil {
		t.Fatal(err)
	}
	// Manifest protects the precise artifact dir.
	l := New(storeWith(t, map[string]string{"coder": served}), &fakeRuntime{})
	// Evicting the parent of a protected artifact must be refused (root under candidate).
	if !l.IsProtected(context.Background(), models) {
		t.Error("evicting a parent dir of a protected artifact must be refused")
	}
	// Evicting a subdir of the protected artifact must be refused (candidate under root).
	if !l.IsProtected(context.Background(), filepath.Join(served, "shard0")) {
		t.Error("evicting a subpath of a protected artifact must be refused")
	}
}

func TestProtected_ExitedContainerDoesNotProtect(t *testing.T) {
	qwen := modelDir(t, "Qwen")
	exited := runtime.ServiceState{Name: "qwen", Running: false, Labels: map[string]string{
		lifecycle.LabelManagedBy:        lifecycle.ManagedByValue,
		lifecycle.LabelArtifactHostPath: qwen,
	}}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{exited}})
	if l.IsProtected(context.Background(), qwen) {
		t.Error("an exited container (no manifest) must not protect its artifact")
	}
}

func TestProtected_CanonicalizesSymlink(t *testing.T) {
	real := modelDir(t, "real")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Manifest records the symlink; query the real path and vice versa.
	l := New(storeWith(t, map[string]string{"qwen": link}), &fakeRuntime{})
	if !l.IsProtected(context.Background(), real) {
		t.Error("the real path must match an artifact recorded via its symlink")
	}
	if !l.IsProtected(context.Background(), link) {
		t.Error("the symlink path must match too")
	}
}

func TestProtectedArtifacts_Union(t *testing.T) {
	a := modelDir(t, "A")
	b := modelDir(t, "B")
	l := New(
		storeWith(t, map[string]string{"a": a}),
		&fakeRuntime{managed: []runtime.ServiceState{runningContainer("b", b)}},
	)
	keys, all := l.ProtectedArtifacts(context.Background())
	if all {
		t.Fatal("should not be allProtected here")
	}
	if len(keys) != 2 {
		t.Errorf("protected union must include both, got %v", keys)
	}
}

func TestInstance_Verdict(t *testing.T) {
	qwen := modelDir(t, "Qwen")
	l := New(storeWith(t, map[string]string{"qwen": qwen}),
		&fakeRuntime{managed: []runtime.ServiceState{runningContainer("qwen", qwen)}})
	il, err := l.Instance(context.Background(), "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if !il.HasManifest || !il.Running || !il.Protected {
		t.Errorf("expected running+desired+protected, got %+v", il)
	}
	il2, _ := l.Instance(context.Background(), "ghost")
	if il2.Protected {
		t.Error("an unknown instance must not be protected")
	}
}

func TestInstance_HonorsSetFailClosed(t *testing.T) {
	// codex Mode-B P1: when the authority is fail-closed (a mystery managed
	// container), a per-instance query for an UNRELATED name must not report
	// evictable.
	bad := runtime.ServiceState{Name: "mystery", Running: true, Labels: map[string]string{
		lifecycle.LabelManagedBy: lifecycle.ManagedByValue,
		lifecycle.LabelInstance:  "mystery",
	}}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{bad}})
	il, err := l.Instance(context.Background(), "some-other-name")
	if err != nil {
		t.Fatal(err)
	}
	if !il.Protected {
		t.Errorf("under set-level fail-closed, an unrelated instance must not be evictable, got %+v", il)
	}
}
