package servecontract

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serveprofiles"
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

// gfx1151 must NOT get --enforce-eager. It was seeded true from vLLM #32180
// without measurement; on the engine this profile is stamped for, HIP graph
// capture completes in 8s, serves, and returns correct output. Emitting the
// flag would impose a throughput cost to prevent a crash that does not happen.
func TestResolve_StrixHalo_DoesNotEmitEnforceEager(t *testing.T) {
	res, err := Resolve(Request{ServedName: "m", Target: strixTarget}, denseFacts())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hasFlag(res.Flags, "--enforce-eager") {
		t.Errorf("gfx1151 must not force eager on this engine build, got %v", res.Flags)
	}
}

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

// No accelerator currently declares EnforceEager, but the LEVER must keep
// working: the failure it guards (graph capture wedging a driver) is real, it
// just does not occur on the engine gfx1151 is stamped for. Exercising
// assembleFlags directly keeps the emission path covered without seeding a
// requirement no measurement supports.
func TestAssembleFlags_EmitsEnforceEagerWhenDeclared(t *testing.T) {
	profile, ok := serveprofiles.Lookup("Qwen3MoeForCausalLM")
	if !ok {
		t.Fatal("fixture arch profile missing")
	}
	r := Request{ServedName: "m", Target: strixTarget}

	with := assembleFlags(r, denseFacts(), profile, nil, 0, 0, true)
	if !hasFlag(with, "--enforce-eager") {
		t.Errorf("a declared enforce-eager must reach the flags, got %v", with)
	}

	without := assembleFlags(r, denseFacts(), profile, nil, 0, 0, false)
	if hasFlag(without, "--enforce-eager") {
		t.Errorf("an undeclared enforce-eager must not appear, got %v", without)
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
// all arch profiles were authored against GB10 -- and conflating the two would
// make this test assert something it does not mean.
func hasEagerHWWarning(warnings []string) bool {
	for _, w := range warnings {
		if strings.Contains(w, `the enforce-eager default for accelerator`) {
			return true
		}
	}
	return false
}
