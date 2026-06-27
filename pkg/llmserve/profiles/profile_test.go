package profiles

import (
	"slices"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

func TestQuantFlagsFor_KnownMethods(t *testing.T) {
	cases := []struct {
		q    serving.QuantMethod
		want []string
	}{
		{serving.QuantNVFP4, nil},
		{serving.QuantFP8, nil},
		{serving.QuantCompressedTensors, nil},
		{serving.QuantNone, nil},
		{serving.QuantGPTQ, []string{"--quantization", "moe_wna16"}},
	}
	for _, c := range cases {
		got, ok := QuantFlagsFor(c.q)
		if !ok {
			t.Errorf("%q should be a known quant method", c.q)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("QuantFlagsFor(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestQuantFlagsFor_UnknownMethod(t *testing.T) {
	if _, ok := QuantFlagsFor(serving.QuantMethod("awq")); ok {
		t.Error("an unseeded quant method must report ok=false so resolution rejects it")
	}
}

func TestLookup_CanonicalAndAlt(t *testing.T) {
	if _, ok := Lookup("Qwen3MoeForCausalLM"); !ok {
		t.Error("canonical Qwen3 MoE arch must resolve")
	}
	if _, ok := Lookup("Qwen3NextForCausalLM"); !ok {
		t.Error("alt arch Qwen3-Next must resolve to the Qwen3 MoE profile")
	}
	if _, ok := Lookup("NopeForCausalLM"); ok {
		t.Error("unknown arch must not resolve")
	}
}

func TestBuiltins_EveryClaimAsserted(t *testing.T) {
	// v1 invariant (§8.0): claims ship hand-seeded and status is `asserted`.
	// A non-asserted status in v1 data means someone hand-wrote a verdict, which
	// only probes (v2) may do.
	for _, p := range BuiltinProfiles() {
		if p.AuthoredAgainst.Zero() {
			t.Errorf("%s: profile must be stamped with the environment it was authored against", p.Arch)
		}
		for _, cl := range p.Claims {
			if cl.Status != StatusAsserted {
				t.Errorf("%s/%s: v1 claim status must be asserted, got %q", p.Arch, cl.Capability, cl.Status)
			}
			if cl.Provenance == "" {
				t.Errorf("%s/%s: claim must carry provenance", p.Arch, cl.Capability)
			}
		}
	}
}
