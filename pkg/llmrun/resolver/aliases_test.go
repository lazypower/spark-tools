package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAliases_Empty(t *testing.T) {
	dir := t.TempDir()

	aliases, err := LoadAliases(dir)
	if err != nil {
		t.Fatalf("LoadAliases failed: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("expected empty aliases, got %d", len(aliases))
	}
}

func TestSetAlias_AndLoad(t *testing.T) {
	dir := t.TempDir()

	if err := SetAlias(dir, "qwen", "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M"); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}

	aliases, err := LoadAliases(dir)
	if err != nil {
		t.Fatalf("LoadAliases failed: %v", err)
	}
	if aliases["qwen"] != "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M" {
		t.Errorf("alias qwen = %q, want bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M", aliases["qwen"])
	}

	// Verify the file was written.
	fp := filepath.Join(dir, "aliases.json")
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("aliases.json not created: %v", err)
	}
}

func TestSetAlias_Update(t *testing.T) {
	dir := t.TempDir()

	if err := SetAlias(dir, "qwen", "old/ref:Q4_K_M"); err != nil {
		t.Fatalf("first SetAlias failed: %v", err)
	}
	if err := SetAlias(dir, "qwen", "new/ref:Q5_K_M"); err != nil {
		t.Fatalf("second SetAlias failed: %v", err)
	}

	aliases, err := LoadAliases(dir)
	if err != nil {
		t.Fatalf("LoadAliases failed: %v", err)
	}
	if aliases["qwen"] != "new/ref:Q5_K_M" {
		t.Errorf("alias qwen = %q, want new/ref:Q5_K_M", aliases["qwen"])
	}
}

func TestSetAlias_EmptyName(t *testing.T) {
	dir := t.TempDir()
	if err := SetAlias(dir, "", "some/ref"); err == nil {
		t.Fatal("expected error for empty alias name")
	}
}

func TestSetAlias_EmptyRef(t *testing.T) {
	dir := t.TempDir()
	if err := SetAlias(dir, "name", ""); err == nil {
		t.Fatal("expected error for empty alias ref")
	}
}

func TestRemoveAlias(t *testing.T) {
	dir := t.TempDir()

	if err := SetAlias(dir, "qwen", "some/ref:Q4_K_M"); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}

	if err := RemoveAlias(dir, "qwen"); err != nil {
		t.Fatalf("RemoveAlias failed: %v", err)
	}

	aliases, err := LoadAliases(dir)
	if err != nil {
		t.Fatalf("LoadAliases failed: %v", err)
	}
	if _, ok := aliases["qwen"]; ok {
		t.Fatal("alias 'qwen' should have been removed")
	}
}

func TestRemoveAlias_NotFound(t *testing.T) {
	dir := t.TempDir()

	if err := RemoveAlias(dir, "nonexistent"); err == nil {
		t.Fatal("expected error removing nonexistent alias")
	}
}

func TestListAliases(t *testing.T) {
	dir := t.TempDir()

	if err := SetAlias(dir, "a", "ref/a:Q4_K_M"); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}
	if err := SetAlias(dir, "b", "ref/b:Q5_K_M"); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}

	aliases, err := ListAliases(dir)
	if err != nil {
		t.Fatalf("ListAliases failed: %v", err)
	}
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases["a"] != "ref/a:Q4_K_M" {
		t.Errorf("alias a = %q, want ref/a:Q4_K_M", aliases["a"])
	}
	if aliases["b"] != "ref/b:Q5_K_M" {
		t.Errorf("alias b = %q, want ref/b:Q5_K_M", aliases["b"])
	}
}

func TestSaveAliases_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")

	if err := SaveAliases(dir, map[string]string{"x": "y"}); err != nil {
		t.Fatalf("SaveAliases failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "aliases.json")); err != nil {
		t.Fatalf("aliases.json not created in nested dir: %v", err)
	}
}

func TestMultipleAliases(t *testing.T) {
	dir := t.TempDir()

	refs := map[string]string{
		"qwen-32b":    "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M",
		"deepseek-r1": "bartowski/DeepSeek-R1-0528-GGUF:Q4_K_M",
		"llama3":      "meta-llama/Llama-3-8B-GGUF:Q5_K_M",
	}

	for name, ref := range refs {
		if err := SetAlias(dir, name, ref); err != nil {
			t.Fatalf("SetAlias(%q) failed: %v", name, err)
		}
	}

	aliases, err := ListAliases(dir)
	if err != nil {
		t.Fatalf("ListAliases failed: %v", err)
	}
	if len(aliases) != 3 {
		t.Fatalf("expected 3 aliases, got %d", len(aliases))
	}
	for name, ref := range refs {
		if aliases[name] != ref {
			t.Errorf("alias %q = %q, want %q", name, aliases[name], ref)
		}
	}
}
