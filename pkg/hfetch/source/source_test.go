package source

import (
	"context"
	"testing"

	"github.com/lazypower/spark-tools/internal/hub"
	hs "github.com/lazypower/spark-tools/internal/hubsource"
)

// The behavior suite (Head uses injected metadata, Download range-fallback
// translation) lives in internal/hubsource; this locks the compat surface (alias
// identity + method ride-along, delegated constructor).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ *hs.File = (*File)(nil)
}

func TestWrapper_NewAndHead(t *testing.T) {
	c := hub.NewClient()
	f := New(c, "org/model", "config.json", 42, "deadbeef")
	size, sha, err := f.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if size != 42 || sha != "deadbeef" {
		t.Errorf("Head must return injected metadata, got size=%d sha=%q", size, sha)
	}
}
