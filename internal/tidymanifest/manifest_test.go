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

// The manifest is a deletion-safety boundary: a misspelled section must be
// rejected, not silently parsed as an empty list that unblesses every model in
// it (a cron'd prune would then delete them).
func TestLoadRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	// "guff:" instead of "gguf:" — the exact typo the strict decode must catch.
	body := "version: 1\nguff:\n  - repo: unsloth/Qwen3-32B-GGUF\n    quant: Q4_K_M\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a parse error for the misspelled section, got nil")
	}
	if !strings.Contains(err.Error(), "manifest parse error") {
		t.Errorf("error should mention parse error: %v", err)
	}
}

// A stray `---` separator must be rejected, not silently drop every entry after
// the first document (which would unbless models and let prune delete them).
func TestLoadRejectsMultipleDocuments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	body := "version: 1\ngguf: []\n---\ngguf:\n  - repo: org/blessed-model\n    quant: Q4_K_M\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a parse error for a multi-document manifest, got nil")
	}
	if !strings.Contains(err.Error(), "manifest parse error") {
		t.Errorf("error should mention parse error: %v", err)
	}
}

// Save must produce a 0644 file, matching the pre-atomic-write behavior rather
// than CreateTemp's 0600 default.
func TestSavePreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := Save(&Manifest{Version: 1, Ollama: []OllamaModelSpec{{Name: "a"}}}, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("manifest mode = %o, want 0644", got)
	}
}

// An empty manifest file is a well-formed empty manifest, not a parse error —
// strict decoding must not regress this.
func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("empty file should load cleanly: %v", err)
	}
	if len(m.Ollama) != 0 || len(m.GGUF) != 0 || len(m.VLLM) != 0 {
		t.Errorf("empty file should yield an empty manifest: %+v", m)
	}
}

// Save must not leave its staging temp file behind after a successful write.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := Save(&Manifest{Version: 1, Ollama: []OllamaModelSpec{{Name: "a"}}}, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("stale temp file left behind: %s", e.Name())
		}
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
