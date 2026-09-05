package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
)

func withDetect(t *testing.T, fn func() string) {
	t.Helper()
	orig := detectAccelerator
	detectAccelerator = fn
	t.Cleanup(func() { detectAccelerator = orig })
}

func TestResolveAccelerator_ExplicitFlagWins(t *testing.T) {
	withDetect(t, func() string { return "amd:strix-halo:gfx1151" })

	var warn bytes.Buffer
	got := resolveAccelerator("nvidia:gb10:sm121", &warn)
	if got != "nvidia:gb10:sm121" {
		t.Errorf("explicit --accelerator must win over detection, got %q", got)
	}
	if warn.Len() != 0 {
		t.Errorf("an explicit flag must not warn, got %q", warn.String())
	}
}

func TestResolveAccelerator_DetectsWhenUnset(t *testing.T) {
	withDetect(t, func() string { return "amd:strix-halo:gfx1151" })

	var warn bytes.Buffer
	if got := resolveAccelerator("", &warn); got != "amd:strix-halo:gfx1151" {
		t.Errorf("unset --accelerator must use detection, got %q", got)
	}
	if warn.Len() != 0 {
		t.Errorf("a successful detection must not warn, got %q", warn.String())
	}
}

func TestResolveAccelerator_BlankFlagIsUnset(t *testing.T) {
	withDetect(t, func() string { return "amd:strix-halo:gfx1151" })

	if got := resolveAccelerator("   ", nil); got != "amd:strix-halo:gfx1151" {
		t.Errorf("a whitespace-only flag must be treated as unset, got %q", got)
	}
}

// When nothing is detected the stamp may be fiction, so the fallback must be
// announced rather than applied silently.
func TestResolveAccelerator_FallbackWarns(t *testing.T) {
	withDetect(t, func() string { return "" })

	var warn bytes.Buffer
	got := resolveAccelerator("", &warn)
	if got != hardware.FallbackAccelerator {
		t.Errorf("got %q, want the fallback %q", got, hardware.FallbackAccelerator)
	}
	if !strings.Contains(warn.String(), hardware.FallbackAccelerator) {
		t.Errorf("fallback must be announced, got %q", warn.String())
	}
	if !strings.Contains(warn.String(), "--accelerator") {
		t.Errorf("warning should tell the operator how to set it explicitly, got %q", warn.String())
	}
}

func TestResolveAccelerator_NilWarnWriterIsSafe(t *testing.T) {
	withDetect(t, func() string { return "" })
	if got := resolveAccelerator("", nil); got != hardware.FallbackAccelerator {
		t.Errorf("got %q, want %q", got, hardware.FallbackAccelerator)
	}
}
