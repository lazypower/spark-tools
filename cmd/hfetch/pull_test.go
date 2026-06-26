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
