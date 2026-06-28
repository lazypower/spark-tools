package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/liveness"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// --- flag/value parsing ----------------------------------------------------

func TestParseCaps(t *testing.T) {
	got, err := parseCaps([]string{"thinking", "tool-calling", "", "vision", "guided-decoding"})
	if err != nil {
		t.Fatalf("parseCaps: %v", err)
	}
	want := []serving.Capability{serving.Thinking, serving.ToolCalling, serving.Vision, serving.GuidedDecoding}
	if len(got) != len(want) {
		t.Fatalf("parseCaps dropped/added entries: got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cap[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := parseCaps([]string{"telepathy"}); err == nil {
		t.Error("an unknown capability must error")
	}
}

func TestParseMounts(t *testing.T) {
	got, err := parseMounts([]string{"models:/models"})
	if err != nil {
		t.Fatalf("parseMounts: %v", err)
	}
	if len(got) != 1 || got[0].Container != "/models" {
		t.Fatalf("unexpected mount parse: %+v", got)
	}
	// Host path is resolved to absolute (the spec runs from elsewhere).
	if !filepath.IsAbs(got[0].Host) {
		t.Errorf("host path must be made absolute, got %q", got[0].Host)
	}
	for _, bad := range []string{"noseparator", ":/onlycontainer", "onlyhost:"} {
		if _, err := parseMounts([]string{bad}); err == nil {
			t.Errorf("invalid mount %q must error", bad)
		}
	}
}

func TestParseTarget(t *testing.T) {
	for _, ok := range []string{"compose", "docker-run", "quadlet"} {
		if _, err := parseTarget(ok); err != nil {
			t.Errorf("%q must be a valid target, got %v", ok, err)
		}
	}
	if _, err := parseTarget("kubernetes"); err == nil {
		t.Error("an unknown target must error")
	}
}

func TestImageRef(t *testing.T) {
	cases := map[string]string{
		"vllm/vllm-openai@v0.23.0":       "vllm/vllm-openai:v0.23.0",       // tag suffix → colon
		"vllm/vllm-openai@sha256:abc123": "vllm/vllm-openai@sha256:abc123", // real digest untouched
		"vllm/vllm-openai:v0.23.0":       "vllm/vllm-openai:v0.23.0",       // plain ref untouched
		"vllm/vllm-openai":               "vllm/vllm-openai",
	}
	for in, want := range cases {
		if got := imageRef(in); got != want {
			t.Errorf("imageRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadRepoTree(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "tree.json")
	if err := os.WriteFile(good, []byte(`[{"path":"config.json","size":10}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := loadRepoTree(good)
	if err != nil || len(files) != 1 {
		t.Fatalf("loadRepoTree(good) = %v, %v", files, err)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{not json"), 0o644)
	if _, err := loadRepoTree(bad); err == nil {
		t.Error("malformed JSON must error")
	}
	if _, err := loadRepoTree(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing file must error")
	}
}

// --- dir resolution (audit: no llm-serve dir-resolution tests existed) -----

func TestDirs_Precedence(t *testing.T) {
	t.Run("LLM_SERVE_HOME wins", func(t *testing.T) {
		t.Setenv("LLM_SERVE_HOME", "/custom/root")
		state, spec, wd := dirs()
		if state != "/custom/root" || spec != "/custom/root/specs" || wd != "/custom/root/watchdog" {
			t.Errorf("LLM_SERVE_HOME layout wrong: %q %q %q", state, spec, wd)
		}
	})
	t.Run("falls back to XDG_STATE_HOME", func(t *testing.T) {
		t.Setenv("LLM_SERVE_HOME", "")
		t.Setenv("XDG_STATE_HOME", "/xdg/state")
		state, _, _ := dirs()
		if state != "/xdg/state/llm-serve" {
			t.Errorf("XDG_STATE_HOME root wrong: %q", state)
		}
	})
	t.Run("falls back to home/.local/state", func(t *testing.T) {
		t.Setenv("LLM_SERVE_HOME", "")
		t.Setenv("XDG_STATE_HOME", "")
		state, _, _ := dirs()
		if !strings.HasSuffix(state, filepath.Join(".local", "state", "llm-serve")) {
			t.Errorf("default root must be under ~/.local/state, got %q", state)
		}
	})
}

// --- liveness --check input/output helpers (the interlock contract) --------

func TestReadLines_SkipsEmptyPreservesSpaces(t *testing.T) {
	in := "/models/a\n\n  /models/b with spaces  \n/models/c\n"
	got, err := readLines(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/models/a", "  /models/b with spaces  ", "/models/c"}
	if len(got) != len(want) {
		t.Fatalf("readLines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q (spaces must be preserved)", i, got[i], want[i])
		}
	}
}

func TestPrintUnmanaged(t *testing.T) {
	var buf bytes.Buffer
	printUnmanaged(&buf, liveness.Report{Unmanaged: []liveness.UnmanagedMount{
		{Container: "rogue-vllm", Mounts: []string{"/data/models"}},
	}})
	out := buf.String()
	if !strings.Contains(out, "rogue-vllm") || !strings.Contains(out, "unmanaged container") {
		t.Errorf("unmanaged warning missing detail, got %q", out)
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]bool{"c": true, "a": true, "b": true})
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("sortedKeys must be sorted, got %v", got)
	}
}

// --- info commands ---------------------------------------------------------

func TestProfilesCmd_Output(t *testing.T) {
	cmd := profilesCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("profiles: %v", err)
	}
	out := buf.String()
	// Must list known archs including the Qwen3.5 text arch and capabilities.
	for _, want := range []string{"Qwen3MoeForCausalLM", "Qwen3_5MoeForConditionalGeneration", "capabilities:"} {
		if !strings.Contains(out, want) {
			t.Errorf("profiles output missing %q", want)
		}
	}
}

func TestTargetsCmd_Output(t *testing.T) {
	cmd := targetsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("targets: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"compose", "docker-run", "quadlet"} {
		if !strings.Contains(out, want) {
			t.Errorf("targets output missing %q", want)
		}
	}
}

func TestRootCmd_Wiring(t *testing.T) {
	root := rootCmd()
	want := map[string]bool{
		"emit": false, "profiles": false, "targets": false, "up": false,
		"down": false, "status": false, "recover": false, "forget": false, "liveness": false,
	}
	for _, c := range root.Commands() {
		want[c.Name()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("root missing subcommand %q", name)
		}
	}
}

func TestStatusCmd_NoInstances(t *testing.T) {
	// Empty state dir → Store.List() is empty → no Docker reconcile happens.
	t.Setenv("LLM_SERVE_HOME", t.TempDir())
	cmd := statusCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(buf.String(), "no managed instances") {
		t.Errorf("empty state must report no instances, got:\n%s", buf.String())
	}
}

func TestPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	printStatus(&buf, lifecycle.InstanceStatus{
		Instance:   instance.Instance{Desired: instance.Desired{Name: "qwen-coder"}},
		Reconciled: lifecycle.Reconciled{Status: lifecycle.StatusServing, Reason: "health ok"},
	})
	out := buf.String()
	for _, want := range []string{"qwen-coder", "serving", "steady", "health ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("printStatus missing %q, got %q", want, out)
		}
	}
}

// --- emit end-to-end: spec→stdout, warning→stderr (audit: pipeable seam) ---

func TestEmitCmd_SeparatesStdoutAndStderr(t *testing.T) {
	dir := t.TempDir()
	// Minimal hfetch-verified-shaped artifact: DetectFacts only needs config.json.
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"architectures":["Qwen3MoeForCausalLM"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := emitCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs([]string{
		"--model-dir", dir,
		"--name", "qwen-test",
		"--image", "vllm/vllm-openai@v0.23.0",
		"--cap", "thinking",
		"--target", "compose",
		"--mount", dir + ":/models",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("emit: %v\nstderr:\n%s", err, errb.String())
	}

	// The launch spec is on stdout and is pipeable — no warning text mixed in.
	spec := out.String()
	if !strings.Contains(spec, "image:") {
		t.Errorf("compose spec must be on stdout, got:\n%s", spec)
	}
	if strings.Contains(spec, "warning:") {
		t.Errorf("stdout must NOT contain warnings (it is piped to a runtime), got:\n%s", spec)
	}
	// The "emitting without --repo-tree" advisory goes to stderr.
	if !strings.Contains(errb.String(), "without --repo-tree") {
		t.Errorf("the no-repo-tree advisory must go to stderr, got:\n%s", errb.String())
	}
}
