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

// fakeRuntime only needs ListManaged for B2.
type fakeRuntime struct {
	managed []runtime.ServiceState
	listErr error
}

func (f *fakeRuntime) Up(context.Context, string, string) error   { return nil }
func (f *fakeRuntime) Down(context.Context, string, string) error { return nil }
func (f *fakeRuntime) Inspect(context.Context, string, string) (runtime.RuntimeState, error) {
	return runtime.RuntimeState{}, nil
}
func (f *fakeRuntime) ListManaged(context.Context) ([]runtime.ServiceState, error) {
	return f.managed, f.listErr
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
