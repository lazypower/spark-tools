package main

import (
	"bytes"
	"strings"
	"testing"
)

func withEngineDetect(t *testing.T, fn func() string) {
	t.Helper()
	orig := detectContainerEngine
	detectContainerEngine = fn
	t.Cleanup(func() { detectContainerEngine = orig })
}

func TestResolveContainerEngine_ExplicitWins(t *testing.T) {
	withEngineDetect(t, func() string { return "podman" })
	if got := resolveContainerEngine("docker", nil); got != "docker" {
		t.Errorf("explicit flag must win over detection, got %q", got)
	}
}

func TestResolveContainerEngine_DetectsWhenUnset(t *testing.T) {
	withEngineDetect(t, func() string { return "podman" })
	if got := resolveContainerEngine("", nil); got != "podman" {
		t.Errorf("unset must use detection, got %q", got)
	}
}

func TestResolveContainerEngine_NormalizesCase(t *testing.T) {
	withEngineDetect(t, func() string { return "docker" })
	if got := resolveContainerEngine("  PODMAN  ", nil); got != "podman" {
		t.Errorf("value should be trimmed and lowercased, got %q", got)
	}
}

// An unrecognized engine must not silently render podman-only flags under a
// docker command word; fall back to the historical default and say so.
func TestResolveContainerEngine_UnknownFallsBackAndWarns(t *testing.T) {
	withEngineDetect(t, func() string { return "podman" })
	var warn bytes.Buffer
	got := resolveContainerEngine("nerdctl", &warn)
	if got != "docker" {
		t.Errorf("unknown engine should fall back to docker, got %q", got)
	}
	if !strings.Contains(warn.String(), "nerdctl") {
		t.Errorf("the warning should name the unrecognized value, got %q", warn.String())
	}
}
