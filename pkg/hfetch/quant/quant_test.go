package quant

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/modelmeta"
)

// The full behavior suite now lives in internal/modelmeta. These lock only the
// compatibility surface: delegation and alias identity.

func TestWrapper_ParseDelegates(t *testing.T) {
	hq := []byte(`{"quantization":{"quant_algo":"NVFP4","kv_cache_quant_algo":"FP8"}}`)
	got := Parse([]byte(`{}`), hq, nil)
	want := modelmeta.ParseQuant([]byte(`{}`), hq, nil)
	if got == nil || want == nil || *got != *want {
		t.Fatalf("quant.Parse must delegate to modelmeta.ParseQuant: got %+v want %+v", got, want)
	}
	if got.String() != "modelopt NVFP4, KV-cache FP8" {
		t.Errorf("aliased Info.String() wrong: %q", got.String())
	}
}

func TestWrapper_InfoIsAlias(t *testing.T) {
	var _ *modelmeta.QuantInfo = (*Info)(nil)
}
