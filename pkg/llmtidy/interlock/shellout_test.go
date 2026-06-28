package interlock

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestLLMServeChecker_ExplicitMissing_FailsClosed is the on-box adversarial probe
// turned regression: an explicitly-configured binary that can't be resolved is a
// misconfiguration, NOT absence — it must return a hard error (caller fails
// closed), never ErrLLMServeAbsent (which would silently disable live-model
// protection on an llm-serve-managed box). Covers both the bin arg and the env.
func TestLLMServeChecker_ExplicitMissing_FailsClosed(t *testing.T) {
	t.Run("via bin arg", func(t *testing.T) {
		_, _, err := LLMServeChecker("/nonexistent/llm-serve")(context.Background(), []string{"/m/A"})
		if err == nil || errors.Is(err, ErrLLMServeAbsent) {
			t.Fatalf("an explicitly-named missing binary must fail closed (real error, not ErrLLMServeAbsent), got %v", err)
		}
	})
	t.Run("via LLM_SERVE_BIN", func(t *testing.T) {
		t.Setenv("LLM_SERVE_BIN", "/nonexistent/llm-serve")
		_, _, err := LLMServeChecker("")(context.Background(), []string{"/m/A"})
		if err == nil || errors.Is(err, ErrLLMServeAbsent) {
			t.Fatalf("a configured-but-missing LLM_SERVE_BIN must fail closed, got %v", err)
		}
	})
}

// TestLLMServeChecker_DefaultMissing_Inactive: with nothing configured and no
// llm-serve on PATH, the box genuinely never ran llm-serve → inactive (the
// pass-through default for ollama/gguf-only users), not a fail-closed error.
func TestLLMServeChecker_DefaultMissing_Inactive(t *testing.T) {
	t.Setenv("LLM_SERVE_BIN", "")
	t.Setenv("PATH", "") // guarantee the default "llm-serve" is unresolvable
	_, _, err := LLMServeChecker("")(context.Background(), []string{"/m/A"})
	if !errors.Is(err, ErrLLMServeAbsent) {
		t.Fatalf("an unconfigured, not-on-PATH llm-serve must be inactive (ErrLLMServeAbsent), got %v", err)
	}
}

func TestSplitNonEmpty_PreservesPathSpaces(t *testing.T) {
	// codex P1: a path with leading/trailing spaces must round-trip exactly, or
	// the echo-match desyncs and a protected path looks evictable. Only empty
	// lines are dropped; spaces in a path are preserved.
	got := splitNonEmpty("/models/foo \n  /models/bar\n\n/models/baz\n")
	want := []string{"/models/foo ", "  /models/bar", "/models/baz"}
	if !slices.Equal(got, want) {
		t.Errorf("splitNonEmpty must preserve path spaces, got %q want %q", got, want)
	}
}

func TestSplitNonEmpty_StripsCarriageReturn(t *testing.T) {
	got := splitNonEmpty("/models/foo\r\n")
	if len(got) != 1 || got[0] != "/models/foo" {
		t.Errorf("a trailing CR should be stripped, got %q", got)
	}
}
