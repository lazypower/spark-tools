package tidymanifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeOllamaName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"qwen2.5-coder:32b", "qwen2.5-coder:32b"},
		{"llama3.3:70b", "llama3.3:70b"},
		{"nomic-embed-text", "nomic-embed-text:latest"},
		{"  qwen3-32b  ", "qwen3-32b:latest"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeOllamaName(tc.in); got != tc.want {
			t.Errorf("NormalizeOllamaName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")

	in := &Manifest{
		Version: 1,
		Ollama: []OllamaModelSpec{
			{Name: "qwen2.5-coder:32b"},
			{Name: "llama3.3:70b"},
		},
		GGUF: []GGUFModelSpec{
			{Repo: "unsloth/Qwen3.5-122B-A10B-GGUF", Quant: "Q4_K_M"},
			{Repo: "mradermacher/Venus-120b-v1.0-i1-GGUF"},
		},
	}

	if err := Save(in, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if out.Version != in.Version {
		t.Errorf("Version mismatch: got %d, want %d", out.Version, in.Version)
	}
	if len(out.Ollama) != 2 || out.Ollama[0].Name != "qwen2.5-coder:32b" {
		t.Errorf("Ollama round-trip mismatch: %+v", out.Ollama)
	}
	if len(out.GGUF) != 2 || out.GGUF[0].Quant != "Q4_K_M" {
		t.Errorf("GGUF round-trip mismatch: %+v", out.GGUF)
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")
	_, err := Load(path)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nollama: [{name: missing-close"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "manifest parse error") {
		t.Errorf("error should mention parse error: %v", err)
	}
}

func TestSaveDefaultsVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "manifest.yaml")
	if err := Save(&Manifest{}, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Version != SchemaVersion {
		t.Errorf("default version not applied: got %d", out.Version)
	}
}

func TestSaveNilManifest(t *testing.T) {
	if err := Save(nil, filepath.Join(t.TempDir(), "m.yaml")); err == nil {
		t.Fatal("expected error for nil manifest")
	}
}
