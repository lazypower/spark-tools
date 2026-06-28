package emit

import (
	"strings"
	"testing"

	sc "github.com/lazypower/spark-tools/internal/servecontract"
	ss "github.com/lazypower/spark-tools/internal/servespec"
)

// The behavior suite (rendering, quoting, labels, round trip) lives in
// internal/servespec; this locks the compat surface (alias identity, the Target
// enum, the embedded WatchdogScript, and delegated render funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ ss.Host = Host{}
	var _ ss.Watchdog = Watchdog{}
	var _ ss.Mount = Mount{}
	var _ ss.Target = TargetCompose
}

func TestWrapper_TargetsAndScript(t *testing.T) {
	if len(Targets()) != len(ss.Targets()) {
		t.Error("Targets must delegate to the authority")
	}
	if TargetCompose != ss.TargetCompose || TargetDockerRun != ss.TargetDockerRun || TargetQuadlet != ss.TargetQuadlet {
		t.Error("Target consts must re-export the authority values")
	}
	if WatchdogScript != ss.WatchdogScript || WatchdogScript == "" {
		t.Error("WatchdogScript must re-export the embedded authority script")
	}
}

func TestWrapper_RenderDelegates(t *testing.T) {
	r := &sc.Resolved{Flags: []string{"--model", "/m"}}
	h := Host{Image: "vllm/vllm-openai:v0.23.0", Port: 8000}
	out, err := Render(TargetCompose, r, h)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "vllm/vllm-openai:v0.23.0") {
		t.Errorf("rendered compose must contain the image, got:\n%s", out)
	}
	// Unknown target must error, same as the authority.
	if _, err := Render(Target("bogus"), r, h); err == nil {
		t.Error("unknown target must error")
	}
}
