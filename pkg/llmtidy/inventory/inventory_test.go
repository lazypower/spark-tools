package inventory

import (
	"testing"

	iinv "github.com/lazypower/spark-tools/internal/inventory"
	"github.com/lazypower/spark-tools/internal/modelstore"
)

// The behavior suite lives in internal/inventory; this locks the compat surface
// (alias identity, method ride-along, delegated funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ iinv.Provider = Provider{}
	var _ iinv.Available = Available{}
	var _ iinv.InstalledModel = InstalledModel{}
	var _ iinv.ModelBackend = BackendOllama
}

func TestWrapper_MethodRideAlongAndDelegation(t *testing.T) {
	if BackendVLLM.String() != "vllm" {
		t.Error("aliased ModelBackend.String must carry over")
	}
	b, err := ParseBackend("gguf")
	if err != nil || b != BackendGGUF {
		t.Errorf("ParseBackend must delegate to the authority, got %v %v", b, err)
	}
}

func TestWrapper_GGUFListDelegates(t *testing.T) {
	// An empty registry yields an empty inventory through the wrapper.
	models, err := GGUFList(modelstore.New(t.TempDir()))
	if err != nil {
		t.Fatalf("GGUFList: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected empty inventory, got %d", len(models))
	}
}
