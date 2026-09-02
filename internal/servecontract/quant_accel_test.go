package servecontract

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serving"
)

// nvfp4Facts is a Blackwell-native FP4 artifact.
func nvfp4Facts() serving.ArtifactFacts {
	return serving.ArtifactFacts{
		ModelID:   "Qwen/Qwen3.6-35B-A3B-NVFP4",
		Revision:  "abc123",
		ModelPath: "/models/hf/Qwen3.6-35B-A3B-NVFP4",
		Arch:      "Qwen3MoeForCausalLM",
		Tokenizer: serving.TokenizerQwen,
		Quant:     serving.QuantNVFP4,
	}
}

// NVFP4 is NVIDIA's FP4 format with CUDA-only kernels. Emitting it against an
// AMD accelerator produces a launch that cannot load, and doing so silently is
// the footgun class the compat rules exist to reject.
func TestResolve_NVFP4OnAMD_IsRejected(t *testing.T) {
	amd := fingerprint.Fingerprint{
		Engine:      "kyuz0/vllm-therock-gfx1151@0.28.0+strix",
		Accelerator: "amd:strix-halo:gfx1151",
	}
	_, err := Resolve(Request{ServedName: "m", Target: amd}, nvfp4Facts())
	if err == nil {
		t.Fatal("NVFP4 on an AMD accelerator must be rejected; it cannot load there")
	}
	rej, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected a RejectionError, got %v", err)
	}
	if rej.Remedy == "" {
		t.Error("a rejection must carry an actionable remedy")
	}
}

// The same artifact on the accelerator it was built for must still resolve.
func TestResolve_NVFP4OnNVIDIA_IsAllowed(t *testing.T) {
	if _, err := Resolve(req("m"), nvfp4Facts()); err != nil {
		t.Fatalf("NVFP4 on NVIDIA must resolve: %v", err)
	}
}

// An accelerator string we cannot parse is not evidence of incompatibility.
// Rejecting on ignorance would break every caller using a free-form target.
func TestResolve_NVFP4_UnknownAcceleratorVendor_NotRejected(t *testing.T) {
	tgt := fingerprint.Fingerprint{Engine: "vllm/vllm-openai@v0.23.0", Accelerator: "some-lab-rig"}
	if _, err := Resolve(Request{ServedName: "m", Target: tgt}, nvfp4Facts()); err != nil {
		t.Fatalf("an unparseable accelerator must not trigger the NVFP4 gate: %v", err)
	}
}

// FP8 is deliberately NOT gated on AMD. gfx1151 has no native FP8, but
// "unverified on this accelerator" is a different claim from "cannot work" --
// vLLM has non-native FP8 paths, and blocking on an untested hypothesis is the
// same error as encoding a guessed ceiling. A measurement earns a rule; a
// suspicion does not.
func TestResolve_FP8OnAMD_NotGatedOnSuspicion(t *testing.T) {
	facts := denseFacts()
	facts.Quant = serving.QuantFP8
	amd := fingerprint.Fingerprint{
		Engine:      "kyuz0/vllm-therock-gfx1151@0.28.0+strix",
		Accelerator: "amd:strix-halo:gfx1151",
	}
	if _, err := Resolve(Request{ServedName: "m", Target: amd}, facts); err != nil {
		t.Fatalf("FP8 on AMD must not be rejected without a measurement showing it fails: %v", err)
	}
}

// The rejection must name the rule so an operator can find it.
func TestResolve_NVFP4OnAMD_NamesTheRule(t *testing.T) {
	amd := fingerprint.Fingerprint{
		Engine:      "kyuz0/vllm-therock-gfx1151@0.28.0+strix",
		Accelerator: "amd:strix-halo:gfx1151",
	}
	_, err := Resolve(Request{ServedName: "m", Target: amd}, nvfp4Facts())
	rej, ok := AsRejection(err)
	if !ok {
		t.Fatalf("expected a rejection, got %v", err)
	}
	if rej.Rule != "nvfp4-requires-nvidia" {
		t.Errorf("rule = %q, want nvfp4-requires-nvidia", rej.Rule)
	}
}
