package gguf

import (
	"testing"

	igguf "github.com/lazypower/spark-tools/internal/gguf"
)

// Full behavior suite lives in internal/gguf; this locks the compat surface.

func TestWrapper_Delegates(t *testing.T) {
	if ParseQuantFromFilename("model-Q4_K_M.gguf") != igguf.ParseQuantFromFilename("model-Q4_K_M.gguf") {
		t.Error("ParseQuantFromFilename must delegate to the authority")
	}
	if !IsGGUF("x.gguf") {
		t.Error("IsGGUF delegation broken")
	}
}

func TestWrapper_AliasesAndTables(t *testing.T) {
	var _ *igguf.GGUFMetadata = (*GGUFMetadata)(nil)
	var _ *igguf.FitResult = (*FitResult)(nil)
	if FitUnknown != igguf.FitUnknown {
		t.Error("FitStatus consts must equal the authority")
	}
	// Shared lookup table — same backing map.
	if len(QuantBitsPerWeight) != len(igguf.QuantBitsPerWeight) {
		t.Error("QuantBitsPerWeight must be the authority's table")
	}
}
