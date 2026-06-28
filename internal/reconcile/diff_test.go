package reconcile

import (
	"testing"
	"time"

	"github.com/lazypower/spark-tools/internal/inventory"
	"github.com/lazypower/spark-tools/internal/tidymanifest"
)

func ollamaModel(name string) inventory.InstalledModel {
	return inventory.InstalledModel{
		Name: name, Backend: inventory.BackendOllama, OllamaName: name,
	}
}

func ggufModel(repo, quant string) inventory.InstalledModel {
	return inventory.InstalledModel{
		Backend: inventory.BackendGGUF, Repo: repo, Quant: quant,
		Name: repo + " " + quant,
	}
}

func TestDiffOllamaExactMatch(t *testing.T) {
	m := &tidymanifest.Manifest{Version: 1, Ollama: []tidymanifest.OllamaModelSpec{
		{Name: "qwen3:32b"},
	}}
	inv := []inventory.InstalledModel{
		ollamaModel("qwen3:32b"),
		ollamaModel("untracked:latest"),
	}
	d := Diff(m, inv)
	if len(d.Blessed) != 1 || d.Blessed[0].OllamaName != "qwen3:32b" {
		t.Errorf("blessed = %+v", d.Blessed)
	}
	if len(d.Untracked) != 1 || d.Untracked[0].OllamaName != "untracked:latest" {
		t.Errorf("untracked = %+v", d.Untracked)
	}
	if len(d.Missing) != 0 {
		t.Errorf("missing = %+v", d.Missing)
	}
}

func TestDiffOllamaLatestImplied(t *testing.T) {
	// Manifest without :latest must match installed with :latest.
	m := &tidymanifest.Manifest{Version: 1, Ollama: []tidymanifest.OllamaModelSpec{
		{Name: "llama3.3"},
	}}
	inv := []inventory.InstalledModel{ollamaModel("llama3.3:latest")}
	d := Diff(m, inv)
	if len(d.Blessed) != 1 || len(d.Missing) != 0 {
		t.Errorf("blessed=%v missing=%v", d.Blessed, d.Missing)
	}
}

func TestDiffOllamaMissingTagged(t *testing.T) {
	m := &tidymanifest.Manifest{Version: 1, Ollama: []tidymanifest.OllamaModelSpec{
		{Name: "llama3.3:8b"},
	}}
	inv := []inventory.InstalledModel{ollamaModel("llama3.3:70b")}
	d := Diff(m, inv)
	if len(d.Blessed) != 0 || len(d.Untracked) != 1 || len(d.Missing) != 1 {
		t.Errorf("blessed=%d untracked=%d missing=%d", len(d.Blessed), len(d.Untracked), len(d.Missing))
	}
	if d.Missing[0].Ollama == nil || d.Missing[0].Ollama.Name != "llama3.3:8b" {
		t.Errorf("missing = %+v", d.Missing[0])
	}
}

func TestDiffGGUFRepoCaseInsensitive(t *testing.T) {
	m := &tidymanifest.Manifest{Version: 1, GGUF: []tidymanifest.GGUFModelSpec{
		{Repo: "Unsloth/Qwen3.5-122B-A10B-GGUF", Quant: "Q4_K_M"},
	}}
	inv := []inventory.InstalledModel{
		ggufModel("unsloth/qwen3.5-122b-a10b-gguf", "Q4_K_M"),
	}
	d := Diff(m, inv)
	if len(d.Blessed) != 1 || len(d.Missing) != 0 {
		t.Errorf("blessed=%v missing=%v", d.Blessed, d.Missing)
	}
}

func TestDiffGGUFQuantSpecificity(t *testing.T) {
	m := &tidymanifest.Manifest{Version: 1, GGUF: []tidymanifest.GGUFModelSpec{
		{Repo: "org/repo", Quant: "Q4_K_M"},
	}}
	inv := []inventory.InstalledModel{
		ggufModel("org/repo", "Q4_K_M"),
		ggufModel("org/repo", "Q5_K_M"),
	}
	d := Diff(m, inv)
	if len(d.Blessed) != 1 || d.Blessed[0].Quant != "Q4_K_M" {
		t.Errorf("blessed = %+v", d.Blessed)
	}
	if len(d.Untracked) != 1 || d.Untracked[0].Quant != "Q5_K_M" {
		t.Errorf("untracked = %+v", d.Untracked)
	}
}

func TestDiffGGUFQuantOmittedBlessesAll(t *testing.T) {
	m := &tidymanifest.Manifest{Version: 1, GGUF: []tidymanifest.GGUFModelSpec{
		{Repo: "org/repo"},
	}}
	inv := []inventory.InstalledModel{
		ggufModel("org/repo", "Q4_K_M"),
		ggufModel("org/repo", "Q5_K_M"),
	}
	d := Diff(m, inv)
	if len(d.Blessed) != 2 || len(d.Untracked) != 0 {
		t.Errorf("blessed=%v untracked=%v", d.Blessed, d.Untracked)
	}
}

func TestDiffNilManifestEverythingUntracked(t *testing.T) {
	inv := []inventory.InstalledModel{ollamaModel("a"), ggufModel("b", "Q4")}
	d := Diff(nil, inv)
	if len(d.Untracked) != 2 || len(d.Blessed) != 0 || len(d.Missing) != 0 {
		t.Errorf("d = %+v", d)
	}
}

func TestModelSpecName(t *testing.T) {
	o := ModelSpec{Backend: inventory.BackendOllama, Ollama: &tidymanifest.OllamaModelSpec{Name: "llama3.3"}}
	if got := o.Name(); got != "llama3.3:latest" {
		t.Errorf("Ollama Name = %q", got)
	}
	g1 := ModelSpec{Backend: inventory.BackendGGUF, GGUF: &tidymanifest.GGUFModelSpec{Repo: "Org/Repo", Quant: "Q4_K_M"}}
	if got := g1.Name(); got != "Org/Repo Q4_K_M" {
		t.Errorf("GGUF Name = %q", got)
	}
	g2 := ModelSpec{Backend: inventory.BackendGGUF, GGUF: &tidymanifest.GGUFModelSpec{Repo: "Org/Repo"}}
	if got := g2.Name(); got != "Org/Repo" {
		t.Errorf("GGUF bare Name = %q", got)
	}
}

func TestPrunePlanFiltersBackend(t *testing.T) {
	d := DiffResult{Untracked: []inventory.InstalledModel{
		ollamaModel("a"),
		ggufModel("b", "Q4_K_M"),
	}}
	ollama := inventory.BackendOllama
	plan := PrunePlan(d, PruneOptions{Backend: &ollama}, time.Now())
	if len(plan) != 1 || plan[0].Backend != inventory.BackendOllama {
		t.Errorf("plan = %+v", plan)
	}
}

func TestPrunePlanFiltersAge(t *testing.T) {
	now := time.Now()
	d := DiffResult{Untracked: []inventory.InstalledModel{
		{Name: "old", Backend: inventory.BackendOllama, OllamaName: "old", Modified: now.Add(-30 * 24 * time.Hour)},
		{Name: "new", Backend: inventory.BackendOllama, OllamaName: "new", Modified: now.Add(-2 * 24 * time.Hour)},
	}}
	plan := PrunePlan(d, PruneOptions{OlderThan: 7 * 24 * time.Hour}, now)
	if len(plan) != 1 || plan[0].OllamaName != "old" {
		t.Errorf("plan should contain only old: %+v", plan)
	}
}

func TestSyncPlanFiltersBackend(t *testing.T) {
	d := DiffResult{Missing: []ModelSpec{
		{Backend: inventory.BackendOllama, Ollama: &tidymanifest.OllamaModelSpec{Name: "a"}},
		{Backend: inventory.BackendGGUF, GGUF: &tidymanifest.GGUFModelSpec{Repo: "b"}},
	}}
	gguf := inventory.BackendGGUF
	plan := SyncPlan(d, SyncOptions{Backend: &gguf})
	if len(plan) != 1 || plan[0].Backend != inventory.BackendGGUF {
		t.Errorf("plan = %+v", plan)
	}
}

func TestTotalSize(t *testing.T) {
	got := TotalSize([]inventory.InstalledModel{{Size: 100}, {Size: 250}})
	if got != 350 {
		t.Errorf("got %d", got)
	}
}
