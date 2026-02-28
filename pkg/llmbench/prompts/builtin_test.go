package prompts

import (
	"testing"
)

func TestLoadBuiltin_AllSets(t *testing.T) {
	expected := map[string]int{
		"short":     20,
		"medium":    15,
		"long":      10,
		"code":      15,
		"reasoning": 10,
	}

	for name, count := range expected {
		t.Run(name, func(t *testing.T) {
			prompts, err := LoadBuiltin(name)
			if err != nil {
				t.Fatalf("LoadBuiltin(%q): %v", name, err)
			}
			if len(prompts) != count {
				t.Errorf("got %d prompts, want %d", len(prompts), count)
				for i, p := range prompts {
					t.Logf("  [%d] %q", i, truncate(p, 60))
				}
			}
			// Each prompt should be non-empty
			for i, p := range prompts {
				if p == "" {
					t.Errorf("prompt[%d] is empty", i)
				}
			}
		})
	}
}

func TestLoadBuiltin_NotFound(t *testing.T) {
	_, err := LoadBuiltin("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent set")
	}
}

func TestBuiltinSets(t *testing.T) {
	sets := BuiltinSets()
	if len(sets) != 5 {
		t.Errorf("got %d sets, want 5", len(sets))
	}
	// Should be sorted by name
	for i := 1; i < len(sets); i++ {
		if sets[i].Name < sets[i-1].Name {
			t.Errorf("sets not sorted: %q < %q", sets[i].Name, sets[i-1].Name)
		}
	}
	for _, s := range sets {
		if s.Count == 0 {
			t.Errorf("set %q has 0 prompts", s.Name)
		}
		if s.Description == "" {
			t.Errorf("set %q has no description", s.Name)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
