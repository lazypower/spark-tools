package fingerprint

import (
	"strings"
	"testing"
)

func TestZero(t *testing.T) {
	if !(Fingerprint{}).Zero() {
		t.Error("empty fingerprint must be Zero")
	}
	if (Fingerprint{Engine: "x"}).Zero() {
		t.Error("a fingerprint with any dimension set is not Zero")
	}
}

func TestDrift_MatchingIsEmpty(t *testing.T) {
	f := Fingerprint{Engine: "vllm@v0.23.0", Accelerator: "nvidia:gb10:sm121"}
	if d := Drift(f, f); len(d) != 0 {
		t.Errorf("identical fingerprints must not drift, got %v", d)
	}
}

func TestDrift_NamesDivergedDimensions(t *testing.T) {
	stamped := Fingerprint{Engine: "vllm@v0.23.0", Accelerator: "nvidia:gb10:sm121"}
	target := Fingerprint{Engine: "vllm@v0.30.0", Accelerator: "nvidia:gb10:sm121"}
	d := Drift(target, stamped)
	if len(d) != 1 {
		t.Fatalf("only engine drifted, want 1 entry, got %v", d)
	}
	if !strings.Contains(d[0], "engine") || !strings.Contains(d[0], "v0.23.0") || !strings.Contains(d[0], "v0.30.0") {
		t.Errorf("drift entry must name dimension, stamped, and target: %q", d[0])
	}
}

func TestDrift_BothDimensions(t *testing.T) {
	stamped := Fingerprint{Engine: "a", Accelerator: "x"}
	target := Fingerprint{Engine: "b", Accelerator: "y"}
	if d := Drift(target, stamped); len(d) != 2 {
		t.Errorf("both dimensions drifted, want 2, got %v", d)
	}
}
