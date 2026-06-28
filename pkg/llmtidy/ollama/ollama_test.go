package ollama

import (
	"testing"

	iollama "github.com/lazypower/spark-tools/internal/ollama"
)

// The behavior suite (httptest-backed) lives in internal/ollama; this locks the
// compat surface (alias identity, method ride-along, delegated constructors).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ *iollama.Client = (*Client)(nil)
	var _ iollama.Model = Model{}
	var _ iollama.TagsResponse = TagsResponse{}
	var _ iollama.PullProgress = PullProgress{}
}

func TestWrapper_ConstructorsAndConsts(t *testing.T) {
	if DefaultHost != iollama.DefaultHost || EnvHost != iollama.EnvHost {
		t.Error("consts must re-export the authority values")
	}
	// New normalizes a bare host; method ride-along proves Host() came across.
	if New("localhost:11434").Host() != iollama.New("localhost:11434").Host() {
		t.Error("New must delegate to the authority and carry Host()")
	}
	if NewFromEnv() == nil {
		t.Error("NewFromEnv must return a client")
	}
}
