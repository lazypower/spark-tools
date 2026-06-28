package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/internal/serving"
)

// The behavior suite (arch/tokenizer/quant/vision/remote-code detection) lives in
// internal/serveartifact; this locks the compat surface — the wrapper's
// DetectFacts delegates and returns the same serving.ArtifactFacts authority type.

func TestWrapper_DetectFactsDelegates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"architectures":["Qwen3MoeForCausalLM"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	facts, err := DetectFacts(dir)
	if err != nil {
		t.Fatalf("DetectFacts: %v", err)
	}
	var _ serving.ArtifactFacts = facts
	if facts.Arch != "Qwen3MoeForCausalLM" {
		t.Errorf("expected detected arch, got %q", facts.Arch)
	}
}

func TestWrapper_DetectFacts_NoConfigErrors(t *testing.T) {
	if _, err := DetectFacts(t.TempDir()); err == nil {
		t.Error("DetectFacts must error when config.json is absent")
	}
}
