package contract

import (
	"errors"
	"testing"

	sc "github.com/lazypower/spark-tools/internal/servecontract"
	"github.com/lazypower/spark-tools/internal/serving"
)

// The behavior suite lives in internal/servecontract; this locks the compat
// surface (alias identity, method ride-along, delegated funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ sc.Request = Request{}
	var _ sc.Resolved = Resolved{}
	var _ *sc.RejectionError = (*RejectionError)(nil)
}

func TestWrapper_RejectionRideAlongAndAsRejection(t *testing.T) {
	// A missing served name rejects; the error must be a *RejectionError reachable
	// through both the wrapper and the authority AsRejection.
	_, err := Resolve(Request{}, serving.ArtifactFacts{})
	if err == nil {
		t.Fatal("empty request must reject")
	}
	if re, ok := AsRejection(err); !ok || re.Error() == "" {
		t.Errorf("AsRejection must extract a *RejectionError with a message, got %v", err)
	}
	var ire *sc.RejectionError
	if !errors.As(err, &ire) {
		t.Error("rejection must satisfy errors.As against the authority type")
	}
}

func TestWrapper_CanonicalCapabilitiesDelegates(t *testing.T) {
	in := []serving.Capability{serving.Vision, serving.Thinking}
	if len(CanonicalCapabilities(in)) != len(sc.CanonicalCapabilities(in)) {
		t.Error("CanonicalCapabilities must delegate to the authority")
	}
}
