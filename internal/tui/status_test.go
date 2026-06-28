package tui

import (
	"strings"
	"testing"
)

// These lock the *content composition* of the status renderers (conditional
// fields, separators, endpoint URLs, context math). They assert on substrings
// rather than exact bytes so lipgloss styling/border chrome stays free to change.

func TestRenderServerHeader_ModelAndQuant(t *testing.T) {
	got := RenderServerHeader(ServerStatus{ModelName: "qwen-coder", Quant: "Q4_K_M"})
	if !strings.Contains(got, "qwen-coder") {
		t.Errorf("header must show model name, got %q", got)
	}
	if !strings.Contains(got, "(Q4_K_M)") {
		t.Errorf("header must show quant in parens, got %q", got)
	}
}

func TestRenderServerHeader_OmitsEmptyQuant(t *testing.T) {
	got := RenderServerHeader(ServerStatus{ModelName: "model-x"})
	if strings.Contains(got, "()") {
		t.Errorf("empty quant must not render parens, got %q", got)
	}
}

func TestRenderServerHeader_ConditionalDetails(t *testing.T) {
	// All three detail fields present.
	full := RenderServerHeader(ServerStatus{
		ModelName: "m", ContextSize: 4096, GPUName: "GB10", Threads: 8,
	})
	for _, want := range []string{"Context: 4096 tokens", "GPU: GB10", "Threads: 8"} {
		if !strings.Contains(full, want) {
			t.Errorf("header missing %q, got %q", want, full)
		}
	}
	// Zero/empty fields are omitted, not rendered with zero values.
	bare := RenderServerHeader(ServerStatus{ModelName: "m"})
	for _, absent := range []string{"Context:", "GPU:", "Threads:"} {
		if strings.Contains(bare, absent) {
			t.Errorf("bare header must omit %q, got %q", absent, bare)
		}
	}
}

func TestRenderServerEndpoints(t *testing.T) {
	got := RenderServerEndpoints("127.0.0.1", 8080)
	base := "http://127.0.0.1:8080"
	for _, want := range []string{
		base + "/v1/chat/completions",
		base + "/v1/completions",
		base + "/v1/models",
		base + "/health",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("endpoints missing %q, got:\n%s", want, got)
		}
	}
}

func TestRenderSessionStats_ContextMath(t *testing.T) {
	got := RenderSessionStats(120, 340, 2048, 4096, 55.5)
	for _, want := range []string{"120", "340", "55.5 tok/s", "50.0%", "(2048/4096)"} {
		if !strings.Contains(got, want) {
			t.Errorf("session stats missing %q, got:\n%s", want, got)
		}
	}
}

func TestJoinPipe(t *testing.T) {
	if got := joinPipe(nil); got != "" {
		t.Errorf("joinPipe(nil) = %q, want empty", got)
	}
	if got := joinPipe([]string{"only"}); got != "only" {
		t.Errorf("single element must not get a separator, got %q", got)
	}
	got := joinPipe([]string{"a", "b", "c"})
	for _, want := range []string{"a", "b", "c", "│"} {
		if !strings.Contains(got, want) {
			t.Errorf("joinPipe missing %q, got %q", want, got)
		}
	}
}
