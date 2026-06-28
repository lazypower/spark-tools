package reconcile

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/inventory"
	irec "github.com/lazypower/spark-tools/internal/reconcile"
	"github.com/lazypower/spark-tools/internal/tidymanifest"
)

// The behavior suite lives in internal/reconcile; this locks the compat surface
// (alias identity, method ride-along, delegated diff/plan funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ irec.ModelSpec = ModelSpec{}
	var _ irec.DiffResult = DiffResult{}
	var _ irec.PruneEvent = PruneEvent{}
	var _ irec.SyncEvent = SyncEvent{}
	var _ irec.PruneOptions = PruneOptions{}
	var _ irec.SyncOptions = SyncOptions{}
	var _ Syncer = irec.Syncer(nil)
}

func TestWrapper_DiffDelegates(t *testing.T) {
	// A nil manifest must classify everything as untracked (the authority's rule).
	im := inventory.InstalledModel{Name: "x", Backend: inventory.BackendGGUF}
	d := Diff(nil, []inventory.InstalledModel{im})
	if len(d.Untracked) != 1 || len(d.Blessed) != 0 {
		t.Errorf("nil-manifest diff must mark all untracked, got %+v", d)
	}
	// And the matching path: a manifest blessing the model moves it to Blessed.
	m := &tidymanifest.Manifest{GGUF: []tidymanifest.GGUFModelSpec{{Repo: "x"}}}
	im2 := inventory.InstalledModel{Repo: "x", Backend: inventory.BackendGGUF}
	if d2 := Diff(m, []inventory.InstalledModel{im2}); len(d2.Blessed) != 1 {
		t.Errorf("manifest match must bless the model, got %+v", d2)
	}
}

func TestWrapper_MethodRideAlong(t *testing.T) {
	s := ModelSpec{Backend: inventory.BackendGGUF, GGUF: &tidymanifest.GGUFModelSpec{Repo: "org/m"}}
	if s.Name() != "org/m" {
		t.Errorf("aliased ModelSpec.Name must carry over, got %q", s.Name())
	}
}
