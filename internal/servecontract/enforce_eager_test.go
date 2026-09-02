package servecontract

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serving"
)

// strixTarget is the environment the AMD hardware profile was measured on, so a
// request against it produces no hardware staleness warning.
var strixTarget = fingerprint.Fingerprint{
	Engine:      "kyuz0/vllm-therock-gfx1151@0.28.0+strix",
	Accelerator: "amd:strix-halo:gfx1151",
}

// denseFacts is an unquantized dense artifact, the shape actually served on the
// AMD box.
func denseFacts() serving.ArtifactFacts {
	return serving.ArtifactFacts{
		ModelID:   "Qwen/Qwen3-1.7B",
		Revision:  "def456",
		ModelPath: "/models/hf/Qwen3-1.7B",
		Arch:      "Qwen3MoeForCausalLM",
		Tokenizer: serving.TokenizerQwen,
		Quant:     serving.QuantNone,
	}
}

func TestResolve_StrixHalo_EmitsEnforceEager(t *testing.T) {
	res, err := Resolve(Request{ServedName: "m", Target: strixTarget}, denseFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !hasFlag(res.Flags, "--enforce-eager") {
		t.Fatalf("gfx1151 launch must carry --enforce-eager, got %v", res.Flags)
	}
}

// The flag must not leak onto accelerators that do not need it -- it costs real
// throughput.
func TestResolve_GB10_OmitsEnforceEager(t *testing.T) {
	res, err := Resolve(req("m"), qwenFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasFlag(res.Flags, "--enforce-eager") {
		t.Errorf("GB10 must not get --enforce-eager, got %v", res.Flags)
	}
}

// An accelerator with no seeded profile must not acquire the flag either.
func TestResolve_UnknownAccelerator_OmitsEnforceEager(t *testing.T) {
	tgt := fingerprint.Fingerprint{Engine: "vllm/vllm-openai@v0.23.0", Accelerator: "amd:mi300:gfx942"}
	res, err := Resolve(Request{ServedName: "m", Target: tgt}, denseFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasFlag(res.Flags, "--enforce-eager") {
		t.Errorf("an accelerator with no authored profile must not get --enforce-eager, got %v", res.Flags)
	}
}

// Drift must warn but must NOT withdraw the guard: eager execution is the
// fail-safe direction, and dropping it on a drifted engine would turn a warning
// into an engine crash.
func TestResolve_StrixHalo_DriftedEngineStillEnforcesEagerAndWarns(t *testing.T) {
	drifted := fingerprint.Fingerprint{
		Engine:      "kyuz0/vllm-therock-gfx1151@0.99.0+future",
		Accelerator: "amd:strix-halo:gfx1151",
	}
	res, err := Resolve(Request{ServedName: "m", Target: drifted}, denseFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !hasFlag(res.Flags, "--enforce-eager") {
		t.Error("a drifted engine must still get the fail-safe flag")
	}
	if !hasEagerHWWarning(res.Warnings) {
		t.Errorf("drift must produce a loud re-verify warning for the enforce-eager default, got %v", res.Warnings)
	}
}

// The matching environment must be quiet, or the warning loses its meaning.
func TestResolve_StrixHalo_NoWarningOnMatchingEnvironment(t *testing.T) {
	res, err := Resolve(Request{ServedName: "m", Target: strixTarget}, denseFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasEagerHWWarning(res.Warnings) {
		t.Errorf("no enforce-eager hardware warning expected on the authored environment, got %v", res.Warnings)
	}
}

// hasEagerHWWarning matches the hardware-profile re-verify notice for the
// enforce-eager knob specifically.
//
// It must not match the ARCH-profile staleness notice, whose generic text also
// contains the words "enforce-eager need". That one fires on every AMD emit --
// all arch profiles were authored against GB10, so any non-NVIDIA target drifts
// them by construction -- and conflating the two would make this test assert
// something it does not mean.
func hasEagerHWWarning(warnings []string) bool {
	for _, w := range warnings {
		if strings.Contains(w, `the enforce-eager default for accelerator`) {
			return true
		}
	}
	return false
}
