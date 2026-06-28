package registry

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/modelstore"
)

// Full behavior suite lives in internal/modelstore; this locks the compat
// surface (alias identity + constructor delegation; methods ride the aliases).

func TestWrapper_Aliases(t *testing.T) {
	var _ *modelstore.Registry = (*Registry)(nil)
	var _ *modelstore.StorageLayout = (*StorageLayout)(nil)
	var _ *modelstore.LocalFile = (*LocalFile)(nil)
}

func TestWrapper_NewDelegates(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)
	if r == nil {
		t.Fatal("New must return a registry")
	}
	if err := r.Load(); err != nil { // method via alias
		t.Fatalf("Load via aliased method failed: %v", err)
	}
	if got := NewStorageLayout(dir).ManifestPath(); got == "" {
		t.Error("NewStorageLayout().ManifestPath() must resolve")
	}
}
