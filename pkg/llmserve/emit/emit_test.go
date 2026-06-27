package emit

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
)

func sampleResolved() *contract.Resolved {
	return &contract.Resolved{
		Flags: []string{
			"--model", "/models/hf/Qwen3.6-35B-A3B-NVFP4",
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
			{Host: "./models", Container: "/models/hf"},
		},
	}
}

func TestCompose_EmitsImagePortAndFlags(t *testing.T) {
	out := Compose(sampleResolved(), sampleHost())
	for _, want := range []string{
		"image: vllm/vllm-openai:v0.23.0",
		`"8000:8000"`,
		"./models:/models/hf:ro",
		"--model",
		"command:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compose output missing %q\n---\n%s", want, out)
		}
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
