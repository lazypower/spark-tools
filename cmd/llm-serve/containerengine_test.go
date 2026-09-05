package main

import (
	"bytes"
	"os"
	"path/filepath"
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

// fakeBin writes an executable stub that records every invocation, so a test can
// prove a binary was NOT run.
func fakeBin(t *testing.T, dir, name, sentinel string) {
	t.Helper()
	script := "#!/bin/sh\necho ran >> " + sentinel + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func withPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir)
}

// Detection must decide on presence alone. Executing docker is not merely
// wasteful: on a socket-activated host any docker command STARTS a daemon the
// operator chose not to run, and having started it the probe would then answer
// "docker" -- rendering the group form that yields a GPU-less container on the
// very host that needed podman.
func TestProbeContainerEngine_NeverInvokesDocker(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "invoked")
	fakeBin(t, dir, "docker", sentinel)
	fakeBin(t, dir, "podman", sentinel)
	withPath(t, dir)

	if got := probeContainerEngine(); got != enginePodman {
		t.Errorf("with both present, detection must prefer podman (the loud failure), got %q", got)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("detection executed a container binary; it must decide on presence alone and never start a daemon")
	}
}

func TestProbeContainerEngine_DockerOnlyHost(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "invoked")
	fakeBin(t, dir, "docker", sentinel)
	withPath(t, dir)

	// The DGX Spark shape: docker-ce, no podman.
	if got := probeContainerEngine(); got != engineDocker {
		t.Errorf("a host with no podman must resolve to docker, got %q", got)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("detection must not execute docker")
	}
}

func TestProbeContainerEngine_NeitherPresent(t *testing.T) {
	withPath(t, t.TempDir())
	if got := probeContainerEngine(); got != engineDocker {
		t.Errorf("with neither present, keep the historical default, got %q", got)
	}
}
