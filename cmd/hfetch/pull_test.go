package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// writeShard writes content to dir/name and returns an LFS ModelFile whose
// OID/Size match — a correctly-downloaded weight file.
func writeShard(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return api.ModelFile{Type: "file", Filename: name,
		LFS: &api.LFS{OID: hex.EncodeToString(sum[:]), Size: int64(len(content))}}
}

func writeConfig(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return api.ModelFile{Type: "file", Filename: name, Size: int64(len(content))}
}

func TestValidateSelected(t *testing.T) {
	known := map[string]int64{"model.safetensors": 100, "config.json": 2}
	if err := validateSelected([]string{"model.safetensors", "config.json"}, known); err != nil {
		t.Errorf("files in the listing should validate: %v", err)
	}
	// A typo'd explicit filename must error, not silently produce a 0-byte file.
	if err := validateSelected([]string{"typo.gguf"}, known); err == nil {
		t.Error("a file absent from the listing must error")
	}
}

func TestResolveDest_VLLMPreset(t *testing.T) {
	profile, output, err := resolveDest("vllm", "gguf", "", "nvidia/Qwen3.6-35B-A3B-NVFP4", "/data")
	if err != nil {
		t.Fatal(err)
	}
	if profile != "vllm" {
		t.Errorf("dest vllm should set profile=vllm, got %q", profile)
	}
	if output != "/data/vllm/models/Qwen3.6-35B-A3B-NVFP4" {
		t.Errorf("unexpected flat output dir: %q", output)
	}
}

func TestResolveDest_ExplicitOutputWins(t *testing.T) {
	_, output, err := resolveDest("vllm", "gguf", "/srv/models/x", "org/model", "/data")
	if err != nil {
		t.Fatal(err)
	}
	if output != "/srv/models/x" {
		t.Errorf("explicit --output should be respected, got %q", output)
	}
}

func TestResolveDest_Empty_NoOp(t *testing.T) {
	profile, output, err := resolveDest("", "gguf", "", "org/model", "/data")
	if err != nil || profile != "gguf" || output != "" {
		t.Errorf("empty dest should be a no-op, got profile=%q output=%q err=%v", profile, output, err)
	}
}

func TestResolveDest_Unknown_Errors(t *testing.T) {
	if _, _, err := resolveDest("ollama", "gguf", "", "org/model", "/data"); err == nil {
		t.Error("unknown --dest must error")
	}
}

func TestReportCompleteness_CompleteModelPasses(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeShard(t, dir, "model.safetensors", "weights"),
		writeConfig(t, dir, "config.json", `{}`),
		writeConfig(t, dir, "tokenizer.json", `{}`),
		writeConfig(t, dir, "generation_config.json", `{}`),
		writeConfig(t, dir, "chat_template.jinja", `x`),
	}
	if err := reportCompleteness(repo, dir, true); err != nil {
		t.Fatalf("complete model should pass the gate: %v", err)
	}
}

func TestReportCompleteness_IncompleteModelFails(t *testing.T) {
	dir := t.TempDir()
	// config.json is in the repo but never written locally → hard-fail.
	repo := []api.ModelFile{
		writeShard(t, dir, "model.safetensors", "weights"),
		writeConfig(t, dir, "tokenizer.json", `{}`),
		{Type: "file", Filename: "config.json", Size: 2},
	}
	if err := reportCompleteness(repo, dir, true); err == nil {
		t.Fatal("incomplete model must fail the gate with a non-nil error")
	}
}
