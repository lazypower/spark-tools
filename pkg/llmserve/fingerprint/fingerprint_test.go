package fingerprint

import (
	"testing"

	ifp "github.com/lazypower/spark-tools/internal/fingerprint"
)

// Full behavior suite lives in internal/fingerprint; this locks the compat
// surface (alias identity + method ride-along + Drift delegation).

func TestWrapper_AliasAndMethods(t *testing.T) {
	var _ ifp.Fingerprint = Fingerprint{}
	if !(Fingerprint{}).Zero() {
		t.Error("aliased Zero() method must report empty fingerprint as zero")
	}
	a := Fingerprint{Engine: "e1", Accelerator: "acc"}
	b := Fingerprint{Engine: "e2", Accelerator: "acc"}
	if len(Drift(a, b)) != len(ifp.Drift(a, b)) {
		t.Error("Drift must delegate to the authority")
	}
}
