package instance

import (
	"errors"
	"testing"

	si "github.com/lazypower/spark-tools/internal/serveinstance"
)

// The behavior suite lives in internal/serveinstance; this locks the compat
// surface (alias identity, method ride-along, sentinel-error identity, and the
// delegated NewStore/ValidName funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ si.Instance = Instance{}
	var _ si.Desired = Desired{}
	var _ si.Operation = Operation{}
	var _ si.Phase = PhaseStarting
	var _ *si.Store = (*Store)(nil)
}

func TestWrapper_SentinelIdentity(t *testing.T) {
	// ErrNotFound must be the SAME value as the authority's, so a Load miss
	// through the wrapper still satisfies errors.Is against either symbol.
	if !errors.Is(ErrNotFound, si.ErrNotFound) {
		t.Fatal("wrapper ErrNotFound must preserve sentinel identity")
	}
	s := NewStore(t.TempDir())
	if _, err := s.Load("nope"); !errors.Is(err, ErrNotFound) || !errors.Is(err, si.ErrNotFound) {
		t.Errorf("Load miss must match both wrapper and authority ErrNotFound, got %v", err)
	}
}

func TestWrapper_MethodRideAlongAndValidName(t *testing.T) {
	in := Instance{Operation: &Operation{Phase: PhaseStarting}}
	if !in.InFlight() {
		t.Error("aliased Instance.InFlight must report a starting manifest in-flight")
	}
	if ValidName("../escape") || !ValidName("ok") {
		t.Error("ValidName must delegate to the authority")
	}
}
