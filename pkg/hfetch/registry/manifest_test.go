package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistryLoadSave(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)

	// Load empty — should create empty manifest.
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected empty list, got %d", len(reg.List()))
	}

	// Add a file.
	reg.AddFile("bartowski/Qwen2.5-Coder-32B-GGUF", LocalFile{
		Filename:     "model-Q4_K_M.gguf",
		Size:         19_000_000_000,
		SHA256:       "abc123",
		Quantization: "Q4_K_M",
		LocalPath:    filepath.Join(tmp, "models", "bartowski--Qwen2.5-Coder-32B-GGUF", "model-Q4_K_M.gguf"),
		Complete:     true,
		DownloadedAt: time.Now(),
	})

	if err := reg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from disk.
	reg2 := New(tmp)
	if err := reg2.Load(); err != nil {
		t.Fatalf("Load after save: %v", err)
	}

	models := reg2.List()
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "bartowski/Qwen2.5-Coder-32B-GGUF" {
		t.Errorf("unexpected model ID: %q", models[0].ID)
	}
	if models[0].Author != "bartowski" {
		t.Errorf("unexpected author: %q", models[0].Author)
	}
	if len(models[0].Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(models[0].Files))
	}
	if models[0].Files[0].Quantization != "Q4_K_M" {
		t.Errorf("unexpected quant: %q", models[0].Files[0].Quantization)
	}
}

func TestRegistryGet(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)
	reg.Load()

	reg.AddFile("org/model-a", LocalFile{Filename: "a.gguf", Complete: true})
	reg.AddFile("org/model-b", LocalFile{Filename: "b.gguf", Complete: true})

	if m := reg.Get("org/model-a"); m == nil {
		t.Error("expected to find model-a")
	}
	if m := reg.Get("org/nonexistent"); m != nil {
		t.Error("expected nil for nonexistent model")
	}
}

func TestRegistryPath(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)
	reg.Load()

	reg.AddFile("org/model", LocalFile{
		Filename:  "model-Q4_K_M.gguf",
		LocalPath: "/data/models/org--model/model-Q4_K_M.gguf",
		Complete:  true,
	})
	reg.AddFile("org/model", LocalFile{
		Filename:  "model-Q8_0.gguf",
		LocalPath: "/data/models/org--model/model-Q8_0.gguf",
		Complete:  true,
	})

	// Specific file.
	path := reg.Path("org/model", "model-Q8_0.gguf")
	if path != "/data/models/org--model/model-Q8_0.gguf" {
		t.Errorf("expected Q8_0 path, got %q", path)
	}

	// Default (first complete).
	path = reg.Path("org/model", "")
	if path != "/data/models/org--model/model-Q4_K_M.gguf" {
		t.Errorf("expected first complete file, got %q", path)
	}

	// Not found.
	path = reg.Path("org/nonexistent", "")
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestRegistryRemoveModel(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)
	reg.Load()

	// Create model directory and file on disk.
	modelDir := reg.ModelDir("org/model")
	os.MkdirAll(modelDir, 0700)
	os.WriteFile(filepath.Join(modelDir, "model.gguf"), []byte("fake"), 0644)

	reg.AddFile("org/model", LocalFile{
		Filename:  "model.gguf",
		LocalPath: filepath.Join(modelDir, "model.gguf"),
		Complete:  true,
	})

	if err := reg.Remove("org/model"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if len(reg.List()) != 0 {
		t.Errorf("expected empty after remove, got %d", len(reg.List()))
	}

	if _, err := os.Stat(modelDir); !os.IsNotExist(err) {
		t.Error("expected model directory to be removed")
	}
}

func TestRegistryRemoveSpecificFile(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)
	reg.Load()

	modelDir := reg.ModelDir("org/model")
	os.MkdirAll(modelDir, 0700)
	os.WriteFile(filepath.Join(modelDir, "a.gguf"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(modelDir, "b.gguf"), []byte("b"), 0644)

	reg.AddFile("org/model", LocalFile{
		Filename:  "a.gguf",
		LocalPath: filepath.Join(modelDir, "a.gguf"),
		Complete:  true,
	})
	reg.AddFile("org/model", LocalFile{
		Filename:  "b.gguf",
		LocalPath: filepath.Join(modelDir, "b.gguf"),
		Complete:  true,
	})

	if err := reg.Remove("org/model", "a.gguf"); err != nil {
		t.Fatalf("Remove file: %v", err)
	}

	m := reg.Get("org/model")
	if m == nil {
		t.Fatal("model should still exist")
	}
	if len(m.Files) != 1 {
		t.Errorf("expected 1 file remaining, got %d", len(m.Files))
	}
	if m.Files[0].Filename != "b.gguf" {
		t.Errorf("expected b.gguf remaining, got %q", m.Files[0].Filename)
	}
}

func TestModelDir(t *testing.T) {
	reg := New("/data")
	dir := reg.ModelDir("bartowski/Qwen2.5-Coder-32B-GGUF")
	expected := "/data/models/bartowski--Qwen2.5-Coder-32B-GGUF"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

func TestGC(t *testing.T) {
	tmp := t.TempDir()
	reg := New(tmp)
	reg.Load()

	// Create a model directory with a complete file and partial artifacts.
	modelDir := filepath.Join(tmp, "models", "org--model")
	os.MkdirAll(modelDir, 0700)

	completePath := filepath.Join(modelDir, "model.gguf")
	os.WriteFile(completePath, []byte("complete model data"), 0644)
	os.WriteFile(filepath.Join(modelDir, "model.gguf.partial"), []byte("partial"), 0644)
	os.WriteFile(filepath.Join(modelDir, "model.gguf.state"), []byte("state"), 0644)
	os.WriteFile(filepath.Join(modelDir, "orphaned.gguf"), []byte("orphan data"), 0644)

	reg.AddFile("org/model", LocalFile{
		Filename:  "model.gguf",
		LocalPath: completePath,
		Complete:  true,
	})

	freed, err := reg.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if freed == 0 {
		t.Error("expected bytes freed from GC")
	}

	// Complete file should remain.
	if _, err := os.Stat(completePath); err != nil {
		t.Error("complete file should not be removed by GC")
	}
	// Partial and orphan should be removed.
	if _, err := os.Stat(filepath.Join(modelDir, "model.gguf.partial")); !os.IsNotExist(err) {
		t.Error("partial file should be removed by GC")
	}
	if _, err := os.Stat(filepath.Join(modelDir, "orphaned.gguf")); !os.IsNotExist(err) {
		t.Error("orphaned file should be removed by GC")
	}
}

// ---------- StorageLayout tests ----------

func TestNewStorageLayout(t *testing.T) {
	sl := NewStorageLayout("/data/hfetch")
	if sl.DataDir != "/data/hfetch" {
		t.Errorf("expected DataDir /data/hfetch, got %q", sl.DataDir)
	}
}

func TestStorageLayoutModelDir(t *testing.T) {
	sl := NewStorageLayout("/data")

	tests := []struct {
		modelID  string
		expected string
	}{
		{"bartowski/Qwen2.5-Coder-32B-GGUF", "/data/models/bartowski--Qwen2.5-Coder-32B-GGUF"},
		{"org/simple-model", "/data/models/org--simple-model"},
		{"no-slash-model", "/data/models/no-slash-model"},
	}

	for _, tt := range tests {
		got := sl.ModelDir(tt.modelID)
		if got != tt.expected {
			t.Errorf("ModelDir(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

func TestStorageLayoutFilePath(t *testing.T) {
	sl := NewStorageLayout("/data")

	got := sl.FilePath("org/model", "model-Q4_K_M.gguf")
	expected := "/data/models/org--model/model-Q4_K_M.gguf"
	if got != expected {
		t.Errorf("FilePath = %q, want %q", got, expected)
	}
}

func TestStorageLayoutPartialPath(t *testing.T) {
	sl := NewStorageLayout("/data")

	got := sl.PartialPath("org/model", "model.gguf")
	expected := "/data/models/org--model/model.gguf.partial"
	if got != expected {
		t.Errorf("PartialPath = %q, want %q", got, expected)
	}
}

func TestStorageLayoutStatePath(t *testing.T) {
	sl := NewStorageLayout("/data")

	got := sl.StatePath("org/model", "model.gguf")
	expected := "/data/models/org--model/model.gguf.state"
	if got != expected {
		t.Errorf("StatePath = %q, want %q", got, expected)
	}
}

func TestStorageLayoutManifestPath(t *testing.T) {
	sl := NewStorageLayout("/data")

	got := sl.ManifestPath()
	expected := "/data/manifest.json"
	if got != expected {
		t.Errorf("ManifestPath = %q, want %q", got, expected)
	}
}

func TestStorageLayoutEnsureModelDir(t *testing.T) {
	tmp := t.TempDir()
	sl := NewStorageLayout(tmp)

	modelID := "org/new-model"
	if err := sl.EnsureModelDir(modelID); err != nil {
		t.Fatalf("EnsureModelDir: %v", err)
	}

	expectedDir := sl.ModelDir(modelID)
	info, err := os.Stat(expectedDir)
	if err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}
}

func TestStorageLayoutEnsureModelDirIdempotent(t *testing.T) {
	tmp := t.TempDir()
	sl := NewStorageLayout(tmp)

	modelID := "org/model"
	// Call twice — second call should not error.
	if err := sl.EnsureModelDir(modelID); err != nil {
		t.Fatalf("first EnsureModelDir: %v", err)
	}
	if err := sl.EnsureModelDir(modelID); err != nil {
		t.Fatalf("second EnsureModelDir: %v", err)
	}
}

func TestStorageLayoutEnsureModelDirCreatesNested(t *testing.T) {
	tmp := t.TempDir()
	sl := NewStorageLayout(filepath.Join(tmp, "deep", "nested"))

	modelID := "org/model"
	if err := sl.EnsureModelDir(modelID); err != nil {
		t.Fatalf("EnsureModelDir: %v", err)
	}

	expectedDir := sl.ModelDir(modelID)
	if _, err := os.Stat(expectedDir); err != nil {
		t.Fatalf("expected nested directory to exist: %v", err)
	}
}

// ---------- extractAuthor tests ----------

func TestExtractAuthorWithSlash(t *testing.T) {
	got := extractAuthor("bartowski/Qwen2.5-Coder-32B-GGUF")
	if got != "bartowski" {
		t.Errorf("expected bartowski, got %q", got)
	}
}

func TestExtractAuthorNoSlash(t *testing.T) {
	got := extractAuthor("model-without-org")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractAuthorEmpty(t *testing.T) {
	got := extractAuthor("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractAuthorMultipleSlashes(t *testing.T) {
	got := extractAuthor("org/sub/model")
	if got != "org" {
		t.Errorf("expected org, got %q", got)
	}
}

func TestExtractAuthorSlashOnly(t *testing.T) {
	got := extractAuthor("/model")
	if got != "" {
		t.Errorf("expected empty string for leading slash, got %q", got)
	}
}

func TestExtractAuthorTrailingSlash(t *testing.T) {
	got := extractAuthor("org/")
	if got != "org" {
		t.Errorf("expected org, got %q", got)
	}
}
