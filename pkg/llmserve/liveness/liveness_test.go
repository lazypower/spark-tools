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

// fakeRuntime only needs ListManaged for B2; the lifecycle methods are unused.
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
	for name, modelDir := range dirs {
		if err := s.Save(instance.Instance{Desired: instance.Desired{Name: name, ModelDir: modelDir}}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestProtected_ByManifestIntent(t *testing.T) {
	// A desired manifest protects its artifact even with no running container.
	l := New(storeWith(t, map[string]string{"qwen": "/srv/models/Qwen"}), &fakeRuntime{})
	if !l.IsProtected(context.Background(), "/srv/models/Qwen") {
		t.Error("a manifest's model dir must be protected (intent)")
	}
	if l.IsProtected(context.Background(), "/srv/models/Other") {
		t.Error("an unreferenced artifact must be evictable")
	}
}

func TestProtected_ByRunningOrphanContainer(t *testing.T) {
	// A running managed container with NO manifest (forget --accept-orphan) still
	// protects its artifact via the live half.
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{
		runningContainer("qwen", "/srv/models/Qwen"),
	}})
	if !l.IsProtected(context.Background(), "/srv/models/Qwen") {
		t.Error("a running managed container must protect its artifact even without a manifest")
	}
}

func TestEvictable_WhenNeitherRunningNorDesired(t *testing.T) {
	l := New(storeWith(t, nil), &fakeRuntime{})
	if l.IsProtected(context.Background(), "/srv/models/Gone") {
		t.Error("an artifact with no manifest and no running container must be evictable")
	}
}

func TestFailClosed_RuntimeUnreachable(t *testing.T) {
	l := New(storeWith(t, nil), &fakeRuntime{listErr: errors.New("docker down")})
	if !l.IsProtected(context.Background(), "/anything") {
		t.Error("docker unreachable must fail closed (everything protected)")
	}
	if _, all := l.ProtectedArtifacts(context.Background()); !all {
		t.Error("ProtectedArtifacts must report allProtected on a runtime error")
	}
}

func TestFailClosed_ManagedContainerMissingArtifactLabel(t *testing.T) {
	// A running managed container we can't map to an artifact ⇒ can't rule out it
	// serving the candidate ⇒ everything protected.
	bad := runtime.ServiceState{Name: "mystery", Running: true, Labels: map[string]string{
		lifecycle.LabelManagedBy: lifecycle.ManagedByValue,
		lifecycle.LabelInstance:  "mystery",
		// no artifact-host-path
	}}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{bad}})
	if !l.IsProtected(context.Background(), "/srv/models/Anything") {
		t.Error("an unmappable managed container must fail closed (everything protected)")
	}
}

func TestProtected_ExitedContainerDoesNotProtect(t *testing.T) {
	// An exited managed container is not using the artifact; only a manifest would
	// protect it.
	exited := runtime.ServiceState{Name: "qwen", Running: false, Labels: map[string]string{
		lifecycle.LabelManagedBy:        lifecycle.ManagedByValue,
		lifecycle.LabelArtifactHostPath: "/srv/models/Qwen",
	}}
	l := New(storeWith(t, nil), &fakeRuntime{managed: []runtime.ServiceState{exited}})
	if l.IsProtected(context.Background(), "/srv/models/Qwen") {
		t.Error("an exited container (no manifest) must not protect its artifact")
	}
}

func TestProtected_CanonicalizesSymlinkAndRelative(t *testing.T) {
	// A symlinked artifact dir must match regardless of which path form the
	// consumer presents.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Manifest records the symlink path; query the real path (and vice versa).
	l := New(storeWith(t, map[string]string{"qwen": link}), &fakeRuntime{})
	if !l.IsProtected(context.Background(), real) {
		t.Error("the real path must match an artifact recorded via its symlink")
	}
	if !l.IsProtected(context.Background(), link) {
		t.Error("the symlink path must match too")
	}
}

func TestProtectedArtifacts_Union(t *testing.T) {
	l := New(
		storeWith(t, map[string]string{"a": "/srv/models/A"}),
		&fakeRuntime{managed: []runtime.ServiceState{runningContainer("b", "/srv/models/B")}},
	)
	keys, all := l.ProtectedArtifacts(context.Background())
	if all {
		t.Fatal("should not be allProtected here")
	}
	if len(keys) != 2 {
		t.Errorf("protected union must include both the manifest and the running container, got %v", keys)
	}
}

func TestInstance_Verdict(t *testing.T) {
	l := New(storeWith(t, map[string]string{"qwen": "/srv/models/Qwen"}),
		&fakeRuntime{managed: []runtime.ServiceState{runningContainer("qwen", "/srv/models/Qwen")}})
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
