package emit

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
)

// TestCompose_RoundTrips parses the emitted compose with a real YAML parser and
// asserts the command list survives serialization byte-for-byte against the
// validated flags. A substring test can miss a scalar that parses but parses
// WRONG (e.g. the JSON kwargs losing a brace, or a numeric being coerced); a
// round-trip catches exactly that — the silent "emitted spec launches with
// different args than validated" failure.
func TestCompose_RoundTrips(t *testing.T) {
	r := &contract.Resolved{
		Flags: []string{
			"--model", "/srv/models/Qwen3.6-35B-A3B-NVFP4",
			"--served-model-name", "qwen-36b-fp4",
			"--dtype", "auto",
			"--max-model-len", "131072",
			"--reasoning-parser", "qwen3",
			"--default-chat-template-kwargs", `{"enable_thinking": true}`,
			"--enable-auto-tool-choice",
			"--tool-call-parser", "qwen3_coder",
			"--quantization", "moe_wna16",
		},
	}
	host := Host{Image: "vllm/vllm-openai:v0.23.0", Port: 8000, Volumes: []Mount{{Host: "/srv/models", Container: "/models/hf"}}}
	// The command in the spec must round-trip to exactly the HOST-SPECIALIZED
	// flags (model rewritten to the container path, container path served as a
	// name) — that is what actually launches.
	want, _ := planLaunch(r, host)
	out := Compose(r, host)

	var doc struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("emitted compose is not valid YAML: %v\n---\n%s", err, out)
	}
	svc, ok := doc.Services["vllm"]
	if !ok {
		t.Fatalf("emitted compose has no vllm service\n---\n%s", out)
	}
	if len(svc.Command) != len(want) {
		t.Fatalf("command arg count %d != planned flag count %d\ngot: %#v", len(svc.Command), len(want), svc.Command)
	}
	for i := range want {
		if svc.Command[i] != want[i] {
			t.Errorf("arg %d: emitted %q, planned %q — serialization altered the flag", i, svc.Command[i], want[i])
		}
	}
}
