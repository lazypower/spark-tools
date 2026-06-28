package interlock

import (
	"context"
	"errors"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

func gguf(name, path string) InstalledModel {
	return InstalledModel{Name: name, Backend: inventory.BackendGGUF, Path: path}
}
func ollama(name string) InstalledModel {
	return InstalledModel{Name: name, Backend: inventory.BackendOllama} // no Path
}

// checker returns a fixed protected set / error.
func checker(protected []string, warnings []string, err error) Checker {
	return func(_ context.Context, _ []string) ([]string, []string, error) {
		return protected, warnings, err
	}
}

func names(ms []InstalledModel) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		out[m.Name] = true
	}
	return out
}

func TestApply_BlocksProtectedKeepsRest(t *testing.T) {
	plan := []InstalledModel{gguf("served", "/m/Served"), gguf("unused", "/m/Unused")}
	res := Apply(context.Background(), plan, checker([]string{"/m/Served"}, nil, nil))
	if !names(res.Keep)["unused"] || names(res.Keep)["served"] {
		t.Errorf("only the unused model should be kept, got keep=%v", names(res.Keep))
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Model.Name != "served" {
		t.Errorf("the served model must be blocked, got %+v", res.Blocked)
	}
}

func TestApply_OllamaPassesThroughUnchecked(t *testing.T) {
	// Ollama has no Path → never sent to llm-serve, always kept.
	called := false
	chk := func(_ context.Context, paths []string) ([]string, []string, error) {
		called = true
		return nil, nil, nil
	}
	res := Apply(context.Background(), []InstalledModel{ollama("llama3:8b")}, chk)
	if called {
		t.Error("a plan with only Ollama models must not call the liveness checker")
	}
	if !names(res.Keep)["llama3:8b"] {
		t.Error("ollama model must be kept")
	}
}

func TestApply_FailClosed_OnCheckError(t *testing.T) {
	plan := []InstalledModel{gguf("a", "/m/A"), gguf("b", "/m/B"), ollama("o")}
	res := Apply(context.Background(), plan, checker(nil, nil, errors.New("docker down")))
	// Both path-based candidates blocked; ollama still kept.
	if len(res.Blocked) != 2 {
		t.Errorf("an undeterminable check must block ALL path-based candidates, got %d blocked", len(res.Blocked))
	}
	if !names(res.Keep)["o"] || names(res.Keep)["a"] || names(res.Keep)["b"] {
		t.Errorf("only ollama should remain in keep, got %v", names(res.Keep))
	}
}

func TestApply_Inactive_WhenLLMServeAbsent(t *testing.T) {
	plan := []InstalledModel{gguf("a", "/m/A")}
	res := Apply(context.Background(), plan, checker(nil, nil, ErrLLMServeAbsent))
	if !res.Inactive {
		t.Error("absent llm-serve must mark the interlock inactive")
	}
	if !names(res.Keep)["a"] || len(res.Blocked) != 0 {
		t.Errorf("with no llm-serve, the plan passes through; got keep=%v blocked=%v", names(res.Keep), res.Blocked)
	}
}

func TestApply_SurfacesWarnings(t *testing.T) {
	plan := []InstalledModel{gguf("a", "/m/A")}
	res := Apply(context.Background(), plan, checker(nil, []string{"unmanaged container holding /m"}, nil))
	if len(res.Warnings) != 1 {
		t.Errorf("complaint warnings must be surfaced, got %v", res.Warnings)
	}
}

func TestApply_GGUFEmptyPath_FailsClosed(t *testing.T) {
	// codex P1: a path-based (GGUF) model with no on-disk path must NOT slip
	// through as Ollama-like — it fails closed (blocked), never pruned unchecked.
	plan := []InstalledModel{{Name: "broken", Backend: inventory.BackendGGUF, Path: ""}}
	res := Apply(context.Background(), plan, checker(nil, nil, errors.New("should not be called")))
	if len(res.Blocked) != 1 || len(res.Keep) != 0 {
		t.Errorf("a GGUF model with no path must be blocked (fail-closed), got keep=%v blocked=%v",
			names(res.Keep), res.Blocked)
	}
}

func TestApply_NoPathBased_NoCheck(t *testing.T) {
	res := Apply(context.Background(), []InstalledModel{ollama("x")}, checker(nil, nil, errors.New("should not be called")))
	if len(res.Keep) != 1 || len(res.Blocked) != 0 {
		t.Errorf("a non-path plan must pass through without a check, got %+v", res)
	}
}
