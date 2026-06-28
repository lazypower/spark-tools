package llmrun

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeLlamaDir creates a directory of stub llama.cpp binaries so NewEngine's
// detection succeeds without a real llama.cpp install (version/help probes fail
// silently and are tolerated).
func fakeLlamaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"llama-server", "llama-cli", "llama-bench"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewEngine_DetectsAndResolves(t *testing.T) {
	bin := fakeLlamaDir(t)
	eng, err := NewEngine(
		WithLlamaDir(bin),
		WithConfigDir(t.TempDir()),
		WithDataDir(t.TempDir()),
		WithHFDataDir(t.TempDir()),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	caps := eng.DetectCapabilities()
	if caps == nil || caps.BinaryDir != bin {
		t.Fatalf("capabilities not wired to the fake bin dir: %+v", caps)
	}
	if !caps.ServerMode {
		t.Error("a dir containing llama-server must report ServerMode")
	}

	// ResolveModel of an existing local .gguf path resolves to that absolute path
	// (the downstream import surface for llm-bench).
	gguf := filepath.Join(t.TempDir(), "model-Q4_K_M.gguf")
	if err := os.WriteFile(gguf, []byte("GGUF"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := eng.ResolveModel(context.Background(), gguf)
	if err != nil {
		t.Fatalf("ResolveModel(local path): %v", err)
	}
	if resolved.Path != gguf {
		t.Errorf("resolved path = %q, want %q", resolved.Path, gguf)
	}
}

func TestBuildCommand_FacadeReExport(t *testing.T) {
	// The facade must pass a RunConfig through to engine.BuildCommand and return
	// a runnable argv. (Internals are covered in pkg/llmrun/engine; this locks the
	// re-export surface.)
	bin := fakeLlamaDir(t)
	caps := Capabilities{BinaryDir: bin, BinaryPath: filepath.Join(bin, "llama-server"), ServerMode: true}
	cmd, _, err := BuildCommand(RunConfig{ModelPath: "/models/x.gguf", Port: 8080}, caps)
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if len(cmd) == 0 {
		t.Fatal("BuildCommand returned an empty argv")
	}
	joined := ""
	for _, a := range cmd {
		joined += a + " "
	}
	if !contains(joined, "/models/x.gguf") {
		t.Errorf("argv must reference the model path, got %v", cmd)
	}
}

func TestRecommend_FacadeReExport(t *testing.T) {
	// Recommend must return a usable config from hardware info without panicking
	// on nil-ish hardware.
	hw, _ := DetectHardware()
	cfg := Recommend(hw)
	if cfg.ContextSize < 0 {
		t.Errorf("recommended context size must be sane, got %d", cfg.ContextSize)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
