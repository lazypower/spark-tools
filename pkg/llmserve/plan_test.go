package llmserve

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/emit"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

func qwenFacts() serving.ArtifactFacts {
	return serving.ArtifactFacts{
		ModelID:   "Qwen/Qwen3.6-35B-A3B-NVFP4",
		Revision:  "abc123",
		ModelPath: "/srv/models/Qwen3.6-35B-A3B-NVFP4",
		Arch:      "Qwen3MoeForCausalLM",
		Tokenizer: serving.TokenizerQwen,
		Quant:     serving.QuantNVFP4,
	}
}

func TestBuildPlan_SpecHasLabelsWatchdogAndContainerPath(t *testing.T) {
	plan, resolved, err := BuildPlan(PlanRequest{
		Name:         "qwen-coder",
		Facts:        qwenFacts(),
		Capabilities: []serving.Capability{ToolCalling, GuidedDecoding},
		Image:        "vllm/vllm-openai@v0.23.0",
		Accelerator:  "nvidia:gb10:sm121",
		Mounts:       []emit.Mount{{Host: "/srv/models", Container: "/models/hf"}},
		WatchdogDir:  "/var/lib/llm-serve/watchdog",
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if resolved == nil {
		t.Fatal("resolved contract must be returned")
	}

	spec := plan.Spec
	for _, want := range []string{
		"managed-by=llm-serve",
		"instance=qwen-coder",
		"watchdog:", // the sidecar
		"/var/lib/llm-serve/watchdog:/watchdog:ro",
		"/models/hf/Qwen3.6-35B-A3B-NVFP4",   // model rewritten to container path
		"spec-hash=" + plan.Desired.SpecHash, // the identity label matches the recorded hash
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("emitted spec missing %q\n---\n%s", want, spec)
		}
	}
	if strings.Contains(spec, "/srv/models/Qwen3.6-35B-A3B-NVFP4") {
		t.Errorf("host path must not survive into --model\n---\n%s", spec)
	}
}

func TestBuildPlan_LabelsMatchReconcileDefinition(t *testing.T) {
	// The labels stamped on the spec MUST equal lifecycle.IdentityLabels(desired) —
	// the single definition reconcile verifies against. Guards drift between
	// stamp-time and verify-time.
	plan, _, err := BuildPlan(PlanRequest{
		Name:        "qwen",
		Facts:       qwenFacts(),
		Image:       "vllm/vllm-openai@v0.23.0",
		Accelerator: "nvidia:gb10:sm121",
		Mounts:      []emit.Mount{{Host: "/srv/models", Container: "/models/hf"}},
		WatchdogDir: "/wd",
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range lifecycle.IdentityLabels(plan.Desired) {
		if !strings.Contains(plan.Spec, k+"="+v) {
			t.Errorf("spec missing identity label %s=%s", k, v)
		}
	}
}

func TestBuildPlan_RejectsBadName(t *testing.T) {
	_, _, err := BuildPlan(PlanRequest{Name: "../escape", Facts: qwenFacts(), Image: "img", Accelerator: "a"})
	if err == nil {
		t.Error("BuildPlan must reject an unsafe instance name")
	}
}
