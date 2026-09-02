package serveprofiles

import (
	"slices"
	"testing"

	"github.com/lazypower/spark-tools/internal/serving"
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
		{serving.QuantModelOptMixed, nil},
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
	// Both Qwen3.5/3.6 lines must resolve — the MoE (qwen-36b-fp4 / qwen-36b /
	// qwen-35b) and the dense (Qwen3.6-27B). The generation is natively
	// multimodal, so both claim vision; a build that ships no processor is caught
	// per-artifact by the vision-requires-processor rule, not by the arch claim.
	for _, arch := range []string{"Qwen3_5MoeForConditionalGeneration", "Qwen3_5ForConditionalGeneration"} {
		p, ok := Lookup(arch)
		if !ok {
			t.Fatalf("%s must resolve to a profile", arch)
		}
		if !p.Supports(serving.Vision) {
			t.Errorf("%s: Qwen3.5/3.6 is natively multimodal — vision must be claimed", arch)
		}
		for _, c := range []serving.Capability{serving.GuidedDecoding, serving.Thinking, serving.ToolCalling} {
			if !p.Supports(c) {
				t.Errorf("%s: dense and MoE share the Qwen3.5/3.6 contract — %s must be claimed", arch, c)
			}
		}
		if p.ToolCallParser != "qwen3_coder" || p.ToolParserRequiresTokenizer != serving.TokenizerQwen {
			t.Errorf("%s: tool calling must go through the tokenizer-gated qwen3_coder parser", arch)
		}
		if p.ReasoningParser != "qwen3" {
			t.Errorf("%s: thinking must go through the qwen3 reasoning parser", arch)
		}
	}
	// GLM-4.7-Flash (Glm4MoeLite) must resolve to the GLM profile, not be refused.
	if _, ok := Lookup("Glm4MoeLiteForCausalLM"); !ok {
		t.Error("Glm4MoeLite (GLM-4.7-Flash) must resolve to the GLM profile")
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
