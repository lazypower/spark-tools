package emit

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
)

// sampleResolved emits --model with the artifact's HOST path, as the contract
// does (it knows nothing about mounts). sampleHost mounts that host dir into the
// container, so the driver must rewrite --model to the container side.
func sampleResolved() *contract.Resolved {
	return &contract.Resolved{
		Flags: []string{
			"--model", "/srv/models/Qwen3.6-35B-A3B-NVFP4",
			"--served-model-name", "qwen-36b-fp4",
			"--dtype", "auto",
			"--default-chat-template-kwargs", `{"enable_thinking": false}`,
		},
	}
}

func sampleHost() Host {
	return Host{
		Image: "vllm/vllm-openai:v0.23.0",
		Port:  8000,
		Volumes: []Mount{
			{Host: "/srv/models", Container: "/models/hf"},
		},
	}
}

func TestCompose_EmitsImagePortAndFlags(t *testing.T) {
	out := Compose(sampleResolved(), sampleHost())
	for _, want := range []string{
		"image: vllm/vllm-openai:v0.23.0",
		`"8000:8000"`,
		"/srv/models:/models/hf:ro",
		"--model",
		"command:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compose output missing %q\n---\n%s", want, out)
		}
	}
}

func TestEmit_RewritesModelToContainerPath(t *testing.T) {
	// codex/operator P1: --model must be the CONTAINER path, not the host path,
	// or the container can't find the model and the spec won't run.
	out := Compose(sampleResolved(), sampleHost())
	if !strings.Contains(out, "/models/hf/Qwen3.6-35B-A3B-NVFP4") {
		t.Errorf("--model must be rewritten to the container path\n---\n%s", out)
	}
	if strings.Contains(out, "/srv/models/Qwen3.6-35B-A3B-NVFP4") {
		t.Errorf("the host path must NOT survive into --model\n---\n%s", out)
	}
}

func TestEmit_ServesContainerPathAsName(t *testing.T) {
	// run.sh parity: serve alias + container path so path-addressed callers resolve.
	flags, _ := planLaunch(sampleResolved(), sampleHost())
	si := flagIndex(flags, "--served-model-name")
	if si < 0 || si+2 >= len(flags) {
		t.Fatalf("expected alias + container path after --served-model-name, got %v", flags)
	}
	if flags[si+1] != "qwen-36b-fp4" || flags[si+2] != "/models/hf/Qwen3.6-35B-A3B-NVFP4" {
		t.Errorf("--served-model-name must be alias + container path, got %q %q", flags[si+1], flags[si+2])
	}
}

func TestEmit_WarnsWhenModelNotMounted(t *testing.T) {
	h := sampleHost()
	h.Volumes = nil // no mount covers the model
	_, warnings := planLaunch(sampleResolved(), h)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "not covered by any volume mount") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unmounted model must warn loudly, got %v", warnings)
	}
	// And the warning rides in-band in the emitted spec.
	if !strings.Contains(Compose(sampleResolved(), h), "not covered by any volume mount") {
		t.Error("unmounted warning must appear in the emitted artifact")
	}
}

func TestContainerPath_RelativeMount(t *testing.T) {
	// A relative mount host resolves against cwd (run.sh's model is reachable from
	// the project dir). Build a model path under cwd/<sub> and a ./<sub> mount.
	cp, ok := containerPath("subdir/models/Foo", []Mount{{Host: "subdir/models", Container: "/models/hf"}})
	if !ok {
		t.Fatal("a model under a relative mount must map")
	}
	if cp != "/models/hf/Foo" {
		t.Errorf("container path = %q, want /models/hf/Foo", cp)
	}
}

func TestCompose_QuotesJSONChatTemplateKwargs(t *testing.T) {
	// The JSON kwargs value contains ':' and braces — emitted as a bare YAML
	// scalar it would be mis-parsed. It must be quoted.
	out := Compose(sampleResolved(), sampleHost())
	if !strings.Contains(out, `"{\"enable_thinking\": false}"`) {
		t.Errorf("JSON chat-template-kwargs must be a quoted YAML scalar\n---\n%s", out)
	}
}

func TestDockerRun_QuotesJSONArg(t *testing.T) {
	out := DockerRun(sampleResolved(), sampleHost())
	if !strings.Contains(out, `'{"enable_thinking": false}'`) {
		t.Errorf("docker run must shell-quote the JSON arg\n---\n%s", out)
	}
	if !strings.Contains(out, "-p 8000:8000") {
		t.Errorf("docker run must publish the port\n---\n%s", out)
	}
}

func TestQuadlet_SingleExecLine(t *testing.T) {
	out := Quadlet(sampleResolved(), sampleHost())
	execLines := 0
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, "Exec=") {
			execLines++
		}
	}
	if execLines != 1 {
		t.Errorf("quadlet must carry exactly one Exec line, got %d\n---\n%s", execLines, out)
	}
	if !strings.Contains(out, "[Container]") || !strings.Contains(out, "Image=vllm/vllm-openai:v0.23.0") {
		t.Errorf("quadlet must be a valid .container unit\n---\n%s", out)
	}
}

func TestEmit_StampsIdentityLabels(t *testing.T) {
	h := sampleHost()
	h.Labels = map[string]string{
		"managed-by":   "llm-serve",
		"instance":     "qwen-coder",
		"contract-key": "Qwen3MoeForCausalLM|qwen|nvfp4|tool-calling|eng|hw",
		"spec-hash":    "deadbeef",
	}
	r := sampleResolved()
	for name, out := range map[string]string{
		"compose":    Compose(r, h),
		"docker-run": DockerRun(r, h),
		"quadlet":    Quadlet(r, h),
	} {
		if !strings.Contains(out, "managed-by=llm-serve") || !strings.Contains(out, "instance=qwen-coder") {
			t.Errorf("%s: identity labels must be stamped on the stack\n---\n%s", name, out)
		}
	}
}

func TestEmit_LabelOrderDeterministic(t *testing.T) {
	// The emitted spec (and thus its content hash) must not depend on map order.
	h := sampleHost()
	h.Labels = map[string]string{"z": "1", "a": "2", "m": "3", "managed-by": "llm-serve"}
	first := Compose(sampleResolved(), h)
	for range 20 {
		if Compose(sampleResolved(), h) != first {
			t.Fatal("compose output must be deterministic regardless of label map order")
		}
	}
	// labels appear sorted by key
	ia := strings.Index(first, "a=2")
	im := strings.Index(first, "m=3")
	iz := strings.Index(first, "z=1")
	if !(ia < im && im < iz) {
		t.Errorf("labels must render in sorted key order; got positions a=%d m=%d z=%d", ia, im, iz)
	}
}

func TestRender_UnknownTargetErrors(t *testing.T) {
	if _, err := Render(Target("helm"), sampleResolved(), sampleHost()); err == nil {
		t.Error("an unknown render target must error, not silently default")
	}
	for _, tgt := range Targets() {
		if _, err := Render(tgt, sampleResolved(), sampleHost()); err != nil {
			t.Errorf("render %q: %v", tgt, err)
		}
	}
}

func TestEmit_StalenessWarningInBand(t *testing.T) {
	r := sampleResolved()
	r.Warnings = []string{"asserted + stale — re-verify: profile X ..."}
	for _, out := range []string{
		Compose(r, sampleHost()),
		DockerRun(r, sampleHost()),
		Quadlet(r, sampleHost()),
	} {
		if !strings.Contains(out, "WARNING") || !strings.Contains(out, "stale") {
			t.Errorf("staleness warning must appear in-band in the emitted artifact\n---\n%s", out)
		}
	}
}
