package resolver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// setupTestRegistry creates a temporary hfetch registry with the given
// models populated in the manifest.
func setupTestRegistry(t *testing.T, models []registry.LocalModel) string {
	t.Helper()
	dataDir := t.TempDir()

	manifest := registry.Manifest{
		SchemaVersion: 1,
		Models:        models,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return dataDir
}

// createDummyGGUF creates a zero-byte file to act as a dummy model.
func createDummyGGUF(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
}

func TestResolveModel_EmptyRef(t *testing.T) {
	r := NewResolver(t.TempDir(), t.TempDir())
	_, err := r.ResolveModel(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestResolveModel_LocalAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	createDummyGGUF(t, modelPath)

	r := NewResolver(t.TempDir(), t.TempDir())
	resolved, err := r.ResolveModel(context.Background(), modelPath)
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	if resolved.Source != ResolveSourceLocalPath {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceLocalPath)
	}
	if resolved.Path != modelPath {
		t.Errorf("path = %q, want %q", resolved.Path, modelPath)
	}
	if resolved.RequestedRef != modelPath {
		t.Errorf("requestedRef = %q, want %q", resolved.RequestedRef, modelPath)
	}
}

func TestResolveModel_LocalRelativePath(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	createDummyGGUF(t, modelPath)

	r := NewResolver(t.TempDir(), t.TempDir())
	// ./ prefix should be detected as local path.
	// Use the absolute path ending in .gguf to test the .gguf suffix detection.
	resolved, err := r.ResolveModel(context.Background(), modelPath)
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}
	if resolved.Source != ResolveSourceLocalPath {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceLocalPath)
	}
}

func TestResolveModel_LocalPath_GGUFSuffix(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "some-model.gguf")
	createDummyGGUF(t, modelPath)

	r := NewResolver(t.TempDir(), t.TempDir())
	resolved, err := r.ResolveModel(context.Background(), modelPath)
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}
	if resolved.Source != ResolveSourceLocalPath {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceLocalPath)
	}
}

func TestResolveModel_LocalPath_NotFound(t *testing.T) {
	r := NewResolver(t.TempDir(), t.TempDir())
	_, err := r.ResolveModel(context.Background(), "/nonexistent/path/model.gguf")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestResolveModel_LocalPath_Directory(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver(t.TempDir(), t.TempDir())
	// A directory path ending with / triggers local path detection.
	_, err := r.ResolveModel(context.Background(), dir+"/")
	if err == nil {
		t.Fatal("expected error for directory path")
	}
}

func TestResolveModel_Alias(t *testing.T) {
	configDir := t.TempDir()

	// Set up a model file for the alias to resolve to.
	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "model.gguf")
	createDummyGGUF(t, modelPath)

	// Set up the registry with the target model.
	regDir := setupTestRegistry(t, []registry.LocalModel{
		{
			ID: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF",
			Files: []registry.LocalFile{
				{
					Filename:     "Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf",
					Quantization: "Q4_K_M",
					LocalPath:    modelPath,
					Complete:     true,
				},
			},
		},
	})

	// Set up an alias.
	if err := SetAlias(configDir, "qwen-32b", "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M"); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}

	r := NewResolver(configDir, regDir)
	resolved, err := r.ResolveModel(context.Background(), "qwen-32b")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	if resolved.Source != ResolveSourceAlias {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceAlias)
	}
	if resolved.RequestedRef != "qwen-32b" {
		t.Errorf("requestedRef = %q, want %q", resolved.RequestedRef, "qwen-32b")
	}
	if resolved.Path != modelPath {
		t.Errorf("path = %q, want %q", resolved.Path, modelPath)
	}
	if resolved.Quant != "Q4_K_M" {
		t.Errorf("quant = %q, want Q4_K_M", resolved.Quant)
	}
}

func TestResolveModel_AliasNotFound(t *testing.T) {
	r := NewResolver(t.TempDir(), t.TempDir())
	_, err := r.ResolveModel(context.Background(), "nonexistent-alias")
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestResolveModel_RegistryRef(t *testing.T) {
	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "model.gguf")
	createDummyGGUF(t, modelPath)

	regDir := setupTestRegistry(t, []registry.LocalModel{
		{
			ID: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF",
			Files: []registry.LocalFile{
				{
					Filename:     "Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf",
					Quantization: "Q4_K_M",
					LocalPath:    modelPath,
					Complete:     true,
				},
			},
		},
	})

	r := NewResolver(t.TempDir(), regDir)
	resolved, err := r.ResolveModel(context.Background(), "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	if resolved.Source != ResolveSourceRegistry {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceRegistry)
	}
	if resolved.Path != modelPath {
		t.Errorf("path = %q, want %q", resolved.Path, modelPath)
	}
	if resolved.RegistryID != "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF" {
		t.Errorf("registryID = %q, want bartowski/Qwen2.5-Coder-32B-Instruct-GGUF", resolved.RegistryID)
	}
	if resolved.Quant != "Q4_K_M" {
		t.Errorf("quant = %q, want Q4_K_M", resolved.Quant)
	}
}

func TestResolveModel_RegistryRefNotFound(t *testing.T) {
	regDir := setupTestRegistry(t, nil)

	r := NewResolver(t.TempDir(), regDir)
	_, err := r.ResolveModel(context.Background(), "nonexistent/model:Q4_K_M")
	if err == nil {
		t.Fatal("expected error for model not in registry")
	}
}

func TestResolveModel_RegistryRefNoQuant(t *testing.T) {
	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "model.gguf")
	createDummyGGUF(t, modelPath)

	regDir := setupTestRegistry(t, []registry.LocalModel{
		{
			ID: "bartowski/SomeModel-GGUF",
			Files: []registry.LocalFile{
				{
					Filename:     "SomeModel-Q4_K_M.gguf",
					Quantization: "Q4_K_M",
					LocalPath:    modelPath,
					Complete:     true,
				},
			},
		},
	})

	r := NewResolver(t.TempDir(), regDir)
	resolved, err := r.ResolveModel(context.Background(), "bartowski/SomeModel-GGUF")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	if resolved.Source != ResolveSourceRegistry {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceRegistry)
	}
	if resolved.Path != modelPath {
		t.Errorf("path = %q, want %q", resolved.Path, modelPath)
	}
}

func TestResolveModel_HFPrefix(t *testing.T) {
	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "model.gguf")
	createDummyGGUF(t, modelPath)

	regDir := setupTestRegistry(t, []registry.LocalModel{
		{
			ID: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF",
			Files: []registry.LocalFile{
				{
					Filename:     "Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf",
					Quantization: "Q4_K_M",
					LocalPath:    modelPath,
					Complete:     true,
				},
			},
		},
	})

	r := NewResolver(t.TempDir(), regDir)
	resolved, err := r.ResolveModel(context.Background(), "hf://bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	if resolved.Source != ResolveSourceHFPull {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceHFPull)
	}
	if resolved.Path != modelPath {
		t.Errorf("path = %q, want %q", resolved.Path, modelPath)
	}
	if resolved.RegistryID != "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF" {
		t.Errorf("registryID = %q, want bartowski/Qwen2.5-Coder-32B-Instruct-GGUF", resolved.RegistryID)
	}
}

func TestResolveModel_HFPrefix_NotLocal(t *testing.T) {
	regDir := setupTestRegistry(t, nil)

	r := NewResolver(t.TempDir(), regDir)
	resolved, err := r.ResolveModel(context.Background(), "hf://bartowski/SomeModel-GGUF")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	// Should return a result with empty path (flagged for pull).
	if resolved.Source != ResolveSourceHFPull {
		t.Errorf("source = %v, want %v", resolved.Source, ResolveSourceHFPull)
	}
	if resolved.Path != "" {
		t.Errorf("path = %q, want empty (flagged for pull)", resolved.Path)
	}
	if resolved.RegistryID != "bartowski/SomeModel-GGUF" {
		t.Errorf("registryID = %q, want bartowski/SomeModel-GGUF", resolved.RegistryID)
	}
}

func TestResolveSource_String(t *testing.T) {
	tests := []struct {
		source ResolveSource
		want   string
	}{
		{ResolveSourceLocalPath, "local_path"},
		{ResolveSourceAlias, "alias"},
		{ResolveSourceRegistry, "registry"},
		{ResolveSourceHFPull, "hf_pull"},
		{ResolveSource(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.source.String(); got != tt.want {
			t.Errorf("ResolveSource(%d).String() = %q, want %q", int(tt.source), got, tt.want)
		}
	}
}

func TestParseRegistryRef(t *testing.T) {
	tests := []struct {
		input     string
		wantID    string
		wantQuant string
	}{
		{"bartowski/Qwen2.5-Coder-32B-GGUF:Q4_K_M", "bartowski/Qwen2.5-Coder-32B-GGUF", "Q4_K_M"},
		{"bartowski/Qwen2.5-Coder-32B-GGUF", "bartowski/Qwen2.5-Coder-32B-GGUF", ""},
		{"org/model:Q5_K_S", "org/model", "Q5_K_S"},
		{"simple", "simple", ""},
	}

	for _, tt := range tests {
		gotID, gotQuant := parseRegistryRef(tt.input)
		if gotID != tt.wantID || gotQuant != tt.wantQuant {
			t.Errorf("parseRegistryRef(%q) = (%q, %q), want (%q, %q)",
				tt.input, gotID, gotQuant, tt.wantID, tt.wantQuant)
		}
	}
}

func TestResolveModel_RegistryRefIncompleteFile(t *testing.T) {
	regDir := setupTestRegistry(t, []registry.LocalModel{
		{
			ID: "org/model",
			Files: []registry.LocalFile{
				{
					Filename:     "model-Q4_K_M.gguf",
					Quantization: "Q4_K_M",
					LocalPath:    "/some/path/model.gguf",
					Complete:     false, // Not complete.
				},
			},
		},
	})

	r := NewResolver(t.TempDir(), regDir)
	_, err := r.ResolveModel(context.Background(), "org/model:Q4_K_M")
	if err == nil {
		t.Fatal("expected error for incomplete file in registry")
	}
}

func TestResolveModel_HFPrefixShortCircuitsBeforeRegistryCheck(t *testing.T) {
	// hf:// should be detected first even though it contains a /
	// (which would otherwise be a registry ref).
	regDir := setupTestRegistry(t, nil)
	r := NewResolver(t.TempDir(), regDir)

	resolved, err := r.ResolveModel(context.Background(), "hf://org/model")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}
	if resolved.Source != ResolveSourceHFPull {
		t.Errorf("source = %v, want %v (hf:// should short-circuit)", resolved.Source, ResolveSourceHFPull)
	}
}
