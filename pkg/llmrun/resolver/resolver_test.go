package resolver

import (
	"testing"

	mref "github.com/lazypower/spark-tools/internal/modelref"
)

// The behavior suite (local/alias/registry/hf:// resolution, alias CRUD) lives in
// internal/modelref; this locks the compat surface (alias identity, method ride-
// along, delegated alias funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ mref.ResolvedModel = ResolvedModel{}
	var _ *mref.Resolver = (*Resolver)(nil)
	var _ mref.ResolveSource = ResolveSourceRegistry
}

func TestWrapper_ResolveSourceStringRideAlong(t *testing.T) {
	if ResolveSourceRegistry.String() != "registry" || ResolveSourceHFPull.String() != "hf_pull" {
		t.Error("aliased ResolveSource.String must carry over")
	}
}

func TestWrapper_AliasCRUDDelegates(t *testing.T) {
	dir := t.TempDir()
	if err := SetAlias(dir, "fast", "org/model"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	got, err := ListAliases(dir)
	if err != nil {
		t.Fatalf("ListAliases: %v", err)
	}
	if got["fast"] != "org/model" {
		t.Errorf("alias round-trip mismatch: %+v", got)
	}
	if err := RemoveAlias(dir, "fast"); err != nil {
		t.Fatalf("RemoveAlias: %v", err)
	}
}
