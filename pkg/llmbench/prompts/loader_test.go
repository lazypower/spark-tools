package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile_TextFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "First prompt\n---\nSecond prompt\n---\nThird prompt"
	os.WriteFile(path, []byte(content), 0644)

	prompts, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(prompts) != 3 {
		t.Errorf("got %d prompts, want 3", len(prompts))
	}
	if prompts[0] != "First prompt" {
		t.Errorf("prompt[0]: got %q", prompts[0])
	}
}

func TestLoadFile_YAMLFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := `prompts:
  - text: "Explain the CAP theorem."
    expected_tokens: 300
  - text: "Write a Go function."
    expected_tokens: 500
`
	os.WriteFile(path, []byte(content), 0644)

	prompts, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(prompts) != 2 {
		t.Errorf("got %d prompts, want 2", len(prompts))
	}
	if prompts[0] != "Explain the CAP theorem." {
		t.Errorf("prompt[0]: got %q", prompts[0])
	}
}

func TestLoadFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0644)

	_, err := LoadFile(path)
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
