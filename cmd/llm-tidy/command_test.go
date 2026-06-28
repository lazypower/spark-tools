package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/manifest"
	"github.com/lazypower/spark-tools/pkg/llmtidy/reconcile"
)

func model(name string, b inventory.ModelBackend, size int64) inventory.InstalledModel {
	return inventory.InstalledModel{Name: name, Backend: b, Size: size}
}

// --- flag/value resolution -------------------------------------------------

func TestResolveBackend(t *testing.T) {
	if got, err := resolveBackend(""); err != nil || got != inventory.BackendUnknown {
		t.Errorf("empty flag must yield BackendUnknown, got %v err=%v", got, err)
	}
	for in, want := range map[string]inventory.ModelBackend{
		"ollama": inventory.BackendOllama,
		"gguf":   inventory.BackendGGUF,
		"vllm":   inventory.BackendVLLM,
	} {
		if got, err := resolveBackend(in); err != nil || got != want {
			t.Errorf("resolveBackend(%q) = %v err=%v, want %v", in, got, err, want)
		}
	}
	if _, err := resolveBackend("nonsense"); err == nil {
		t.Error("an unknown backend flag must error")
	}
}

func TestModelsBy(t *testing.T) {
	all := []inventory.InstalledModel{
		model("a", inventory.BackendOllama, 1),
		model("b", inventory.BackendGGUF, 1),
		model("c", inventory.BackendOllama, 1),
	}
	got := modelsBy(all, inventory.BackendOllama)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("modelsBy must keep only the requested backend, got %+v", got)
	}
}

// --- prune planning seam ---------------------------------------------------

func TestPruneBuildPlan_BackendFilter(t *testing.T) {
	d := reconcile.DiffResult{Untracked: []inventory.InstalledModel{
		model("o", inventory.BackendOllama, 10),
		model("g", inventory.BackendGGUF, 20),
		model("v", inventory.BackendVLLM, 30),
	}}

	// BackendUnknown → no filter → all three untracked become candidates.
	all := pruneBuildPlan(d, pruneOpts{backend: inventory.BackendUnknown})
	if len(all) != 3 {
		t.Errorf("unfiltered plan must include all untracked, got %d", len(all))
	}

	// A specific backend restricts the plan to that backend only.
	onlyG := pruneBuildPlan(d, pruneOpts{backend: inventory.BackendGGUF})
	if len(onlyG) != 1 || onlyG[0].Name != "g" {
		t.Errorf("gguf filter must keep only the gguf model, got %+v", onlyG)
	}
}

func TestPruneBuildPlan_OlderThan(t *testing.T) {
	now := time.Now()
	d := reconcile.DiffResult{Untracked: []inventory.InstalledModel{
		{Name: "fresh", Backend: inventory.BackendGGUF, Modified: now.Add(-time.Hour)},
		{Name: "stale", Backend: inventory.BackendGGUF, Modified: now.Add(-90 * 24 * time.Hour)},
	}}
	plan := pruneBuildPlan(d, pruneOpts{backend: inventory.BackendUnknown, olderThan: 30 * 24 * time.Hour})
	if len(plan) != 1 || plan[0].Name != "stale" {
		t.Errorf("older-than must keep only models older than the cutoff, got %+v", plan)
	}
}

// --- prune output contract -------------------------------------------------

func TestRenderPrunePlan_GroupsAndTotal(t *testing.T) {
	var buf bytes.Buffer
	renderPrunePlan(&buf, []inventory.InstalledModel{
		model("llama3:8b", inventory.BackendOllama, 4*1024*1024*1024),
		model("repo/model.gguf", inventory.BackendGGUF, 2*1024*1024*1024),
		model("vllm-model", inventory.BackendVLLM, 6*1024*1024*1024),
	})
	out := buf.String()
	for _, want := range []string{
		"Ollama:", "llama3:8b", "4.0 GB",
		"GGUF:", "repo/model.gguf", "2.0 GB",
		"vLLM:", "vllm-model", "6.0 GB",
		"Total to reclaim: 12.0 GB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prune plan output missing %q, got:\n%s", want, out)
		}
	}
}

// --- status rendering ------------------------------------------------------

func TestRenderBackend_NotAvailable(t *testing.T) {
	var buf bytes.Buffer
	renderBackend(&buf, "Ollama Models", inventory.BackendOllama, reconcile.DiffResult{}, false, time.Now())
	if !strings.Contains(buf.String(), "backend not available") {
		t.Errorf("an unavailable, empty backend must say so, got:\n%s", buf.String())
	}
}

func TestRenderBackend_BlessedAndUntracked(t *testing.T) {
	var buf bytes.Buffer
	d := reconcile.DiffResult{
		Blessed:   []inventory.InstalledModel{model("kept", inventory.BackendGGUF, 1024*1024*1024)},
		Untracked: []inventory.InstalledModel{model("extra", inventory.BackendGGUF, 2*1024*1024*1024)},
	}
	renderBackend(&buf, "GGUF Models", inventory.BackendGGUF, d, true, time.Now())
	out := buf.String()
	for _, want := range []string{"1 blessed, 1 untracked", "kept", "extra", "BLESSED", "UNTRACKED", "Untracked total"} {
		if !strings.Contains(out, want) {
			t.Errorf("backend render missing %q, got:\n%s", want, out)
		}
	}
}

func TestHasMissing(t *testing.T) {
	if hasMissing(reconcile.DiffResult{}) {
		t.Error("empty diff has nothing missing")
	}
	if !hasMissing(reconcile.DiffResult{Missing: []reconcile.ModelSpec{{Backend: inventory.BackendGGUF}}}) {
		t.Error("a diff with a missing spec must report missing")
	}
}

// --- status JSON contract (audit: no golden test existed) ------------------

func TestEmitStatusJSON_Contract(t *testing.T) {
	d := reconcile.DiffResult{
		Blessed:   []inventory.InstalledModel{model("ol", inventory.BackendOllama, 100)},
		Untracked: []inventory.InstalledModel{model("vl", inventory.BackendVLLM, 200)},
		Missing: []reconcile.ModelSpec{{
			Backend: inventory.BackendGGUF,
			GGUF:    &manifest.GGUFModelSpec{Repo: "org/model", Quant: "Q4_K_M"},
		}},
	}
	avail := inventory.Available{Ollama: true, GGUF: false, VLLM: true}

	var buf bytes.Buffer
	if err := emitStatusJSON(&buf, d, avail, errors.New("gguf scan failed")); err != nil {
		t.Fatalf("emitStatusJSON: %v", err)
	}

	// Output must be valid, parseable JSON — the machine-readable contract.
	var got statusJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("status --json must emit valid JSON: %v\n%s", err, buf.String())
	}
	if len(got.Ollama.Blessed) != 1 || got.Ollama.Blessed[0].Name != "ol" {
		t.Errorf("ollama blessed partition wrong: %+v", got.Ollama)
	}
	if len(got.VLLM.Untracked) != 1 || got.VLLM.Untracked[0].Name != "vl" {
		t.Errorf("vllm untracked partition wrong: %+v", got.VLLM)
	}
	if len(got.Missing) != 1 || got.Missing[0].Repo != "org/model" || got.Missing[0].Quant != "Q4_K_M" {
		t.Errorf("missing spec partition wrong: %+v", got.Missing)
	}
	if got.Available.Ollama != true || got.Available.GGUF != false || got.Available.VLLM != true {
		t.Errorf("availability flags wrong: %+v", got.Available)
	}
	if got.Note != "gguf scan failed" {
		t.Errorf("inventory error must surface as note, got %q", got.Note)
	}
}

// --- root command wiring ---------------------------------------------------

func TestRootCmd_Wiring(t *testing.T) {
	root := rootCmd()
	want := map[string]bool{"status": false, "prune": false, "sync": false, "promote": false, "demote": false, "init": false}
	for _, c := range root.Commands() {
		want[c.Name()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("root command missing subcommand %q", name)
		}
	}
	if root.PersistentFlags().Lookup("manifest") == nil {
		t.Error("root must expose the --manifest persistent flag")
	}
	if root.PersistentFlags().Lookup("ollama-host") == nil {
		t.Error("root must expose the --ollama-host persistent flag")
	}
}

func TestCountSpecs(t *testing.T) {
	m := &llmtidy.Manifest{
		Ollama: []manifest.OllamaModelSpec{{Name: "a"}, {Name: "b"}},
		GGUF:   []manifest.GGUFModelSpec{{Repo: "x"}},
	}
	if ol, gg := countSpecs(m, inventory.BackendUnknown); ol != 2 || gg != 1 {
		t.Errorf("unfiltered counts = %d/%d, want 2/1", ol, gg)
	}
	if ol, gg := countSpecs(m, inventory.BackendOllama); ol != 2 || gg != 0 {
		t.Errorf("ollama-only counts = %d/%d, want 2/0", ol, gg)
	}
	if ol, gg := countSpecs(m, inventory.BackendGGUF); ol != 0 || gg != 1 {
		t.Errorf("gguf-only counts = %d/%d, want 0/1", ol, gg)
	}
}

// hermeticTidy builds a Tidy whose backends are inert: GGUF/vLLM registry points
// at an empty temp data dir, and Ollama at a local httptest server reporting an
// empty model list. This exercises the real runStatus/runSync flows without any
// live Ollama/Docker/network dependency.
func hermeticTidy(t *testing.T, manifestBody string) *llmtidy.Tidy {
	t.Helper()
	t.Setenv("HFETCH_DATA_DIR", t.TempDir())

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty model list — Ollama is "up" but holds nothing.
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	t.Cleanup(ollama.Close)

	mpath := filepath.Join(t.TempDir(), "manifest.yaml")
	if manifestBody != "" {
		if err := os.WriteFile(mpath, []byte(manifestBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy, err := llmtidy.New(
		llmtidy.WithManifestPath(mpath),
		llmtidy.WithOllamaHost(ollama.URL),
	)
	if err != nil {
		t.Fatalf("build hermetic tidy: %v", err)
	}
	return tidy
}

func TestRunStatus_MissingSpecRendered(t *testing.T) {
	tidy := hermeticTidy(t, "version: 1\ngguf:\n  - repo: org/missing-model\n    quant: Q4_K_M\n")
	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, tidy, inventory.BackendUnknown, false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"GGUF Models", "MISSING", "org/missing-model"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q, got:\n%s", want, out)
		}
	}
}

func TestRunStatus_JSONIsValid(t *testing.T) {
	tidy := hermeticTidy(t, "version: 1\ngguf:\n  - repo: org/missing-model\n")
	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, tidy, inventory.BackendUnknown, true); err != nil {
		t.Fatalf("runStatus json: %v", err)
	}
	var got statusJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("status --json (with a present manifest) must be valid JSON: %v\n%s", err, buf.String())
	}
	if len(got.Missing) != 1 || got.Missing[0].Repo != "org/missing-model" {
		t.Errorf("missing spec not in JSON: %+v", got.Missing)
	}
}

func TestRunSync_DryRunPlan(t *testing.T) {
	tidy := hermeticTidy(t, "version: 1\ngguf:\n  - repo: org/missing-model\n    quant: Q4_K_M\n")
	var buf bytes.Buffer
	if err := runSync(context.Background(), &buf, tidy, inventory.BackendUnknown, true); err != nil {
		t.Fatalf("runSync dry-run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"will be pulled", "org/missing-model", "dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("sync dry-run missing %q, got:\n%s", want, out)
		}
	}
}

func TestRunSync_AlreadyInSync(t *testing.T) {
	// Empty manifest → nothing missing → "Already in sync."
	tidy := hermeticTidy(t, "version: 1\ngguf: []\n")
	var buf bytes.Buffer
	if err := runSync(context.Background(), &buf, tidy, inventory.BackendUnknown, true); err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if !strings.Contains(buf.String(), "Already in sync") {
		t.Errorf("empty manifest must be already in sync, got:\n%s", buf.String())
	}
}

func TestRunStatus_MissingManifestGuidance(t *testing.T) {
	// No manifest file written → guidance to run init, no crash.
	tidy := hermeticTidy(t, "")
	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, tidy, inventory.BackendUnknown, false); err != nil {
		t.Fatalf("runStatus with no manifest must not error: %v", err)
	}
	if !strings.Contains(buf.String(), "llm-tidy init") {
		t.Errorf("missing manifest must guide to init, got:\n%s", buf.String())
	}
}

func TestRunPrune_NothingToPrune(t *testing.T) {
	// Hermetic inventory is empty → no untracked models → nothing to prune.
	// noInterlock + dryRun keep this free of the llm-serve shellout and TTY confirm.
	tidy := hermeticTidy(t, "version: 1\ngguf: []\n")
	var buf bytes.Buffer
	err := runPrune(context.Background(), &buf, tidy, pruneOpts{
		backend: inventory.BackendUnknown, dryRun: true, noInterlock: true,
	})
	if err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	if !strings.Contains(buf.String(), "Nothing to prune") {
		t.Errorf("empty inventory must prune nothing, got:\n%s", buf.String())
	}
}

func TestRunPrune_DryRunWithUntracked_InterlockInactive(t *testing.T) {
	// Seed the GGUF registry with a completed file absent from the manifest →
	// untracked → a prune candidate. Force llm-serve absent (empty PATH, no
	// LLM_SERVE_BIN) so the interlock is deterministically INACTIVE: the plan is
	// rendered, the inactive notice prints, and dry-run removes nothing.
	dataDir := t.TempDir()
	t.Setenv("HFETCH_DATA_DIR", dataDir)
	t.Setenv("LLM_SERVE_BIN", "")
	t.Setenv("PATH", "")

	ggufPath := filepath.Join(dataDir, "model.gguf")
	if err := os.WriteFile(ggufPath, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(dataDir)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	reg.AddFile("org/model", registry.LocalFile{
		Filename: "model.gguf", Size: 7, Quantization: "Q4_K_M",
		LocalPath: ggufPath, Complete: true, DownloadedAt: time.Now(),
	})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	t.Cleanup(ollama.Close)

	mpath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(mpath, []byte("version: 1\ngguf: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tidy, err := llmtidy.New(llmtidy.WithManifestPath(mpath), llmtidy.WithOllamaHost(ollama.URL))
	if err != nil {
		t.Fatalf("build tidy: %v", err)
	}

	var buf bytes.Buffer
	if err := runPrune(context.Background(), &buf, tidy, pruneOpts{
		backend: inventory.BackendUnknown, dryRun: true, noInterlock: false,
	}); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"org/model", "interlock inactive", "dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("prune dry-run output missing %q, got:\n%s", want, out)
		}
	}
	// Dry-run must not delete the file.
	if _, err := os.Stat(ggufPath); err != nil {
		t.Errorf("dry-run must not remove the model file: %v", err)
	}
}

func TestInitCmd_WritesManifest(t *testing.T) {
	// Drive the real cobra command end to end: hermetic Ollama via OLLAMA_HOST,
	// empty GGUF registry via HFETCH_DATA_DIR. init scans both and writes a manifest.
	t.Setenv("HFETCH_DATA_DIR", t.TempDir())
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	t.Cleanup(ollama.Close)
	t.Setenv("OLLAMA_HOST", ollama.URL)

	mpath := filepath.Join(t.TempDir(), "out.yaml")
	root := rootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"init", "-o", mpath})
	if err := root.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(mpath); err != nil {
		t.Fatalf("init must write a manifest at %s: %v", mpath, err)
	}
	for _, want := range []string{"Scanned", "Wrote manifest to " + mpath} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("init output missing %q, got:\n%s", want, buf.String())
		}
	}
}

// runRootHermetic drives the real root command with hermetic backends
// (empty Ollama via OLLAMA_HOST, empty GGUF registry via HFETCH_DATA_DIR).
func runRootHermetic(t *testing.T, manifestBody string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HFETCH_DATA_DIR", t.TempDir())
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	t.Cleanup(ollama.Close)
	t.Setenv("OLLAMA_HOST", ollama.URL)

	mpath := filepath.Join(t.TempDir(), "manifest.yaml")
	if manifestBody != "" {
		if err := os.WriteFile(mpath, []byte(manifestBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := rootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"--manifest", mpath}, args...))
	err := root.Execute()
	return buf.String(), err
}

func TestPromote_MissingModelErrors(t *testing.T) {
	// Promoting a model absent from inventory is a real error path.
	_, err := runRootHermetic(t, "version: 1\ngguf: []\n", "promote", "ghost-model")
	if err == nil {
		t.Fatal("promote of a model not in inventory must error")
	}
}

func TestStatusCmd_GlueRendersSections(t *testing.T) {
	out, err := runRootHermetic(t, "version: 1\ngguf: []\n", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "Ollama Models") || !strings.Contains(out, "GGUF Models") {
		t.Errorf("status must render backend sections, got:\n%s", out)
	}
}

func TestNewTidy_ManifestPrecedence(t *testing.T) {
	mkCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("manifest", "", "")
		c.Flags().String("ollama-host", "", "")
		return c
	}

	// --manifest flag is honored when no explicit override is given.
	c := mkCmd()
	_ = c.Flags().Set("manifest", "/tmp/from-flag.yaml")
	tidy, err := newTidy(c)
	if err != nil {
		t.Fatalf("newTidy: %v", err)
	}
	if tidy.ManifestPath() != "/tmp/from-flag.yaml" {
		t.Errorf("--manifest flag must drive the path, got %q", tidy.ManifestPath())
	}

	// An explicit override (init --output) beats the persistent flag.
	c2 := mkCmd()
	_ = c2.Flags().Set("manifest", "/tmp/from-flag.yaml")
	tidy2, err := newTidyWithOverride(c2, "/tmp/override.yaml")
	if err != nil {
		t.Fatalf("newTidyWithOverride: %v", err)
	}
	if tidy2.ManifestPath() != "/tmp/override.yaml" {
		t.Errorf("override must beat --manifest flag, got %q", tidy2.ManifestPath())
	}
}
