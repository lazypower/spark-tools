package llmtidy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/manifest"
	"github.com/lazypower/spark-tools/pkg/llmtidy/ollama"
)

// newTestTidy builds a Tidy that talks to the given httptest server for
// Ollama and uses a temp dir for both the manifest and the hfetch registry.
func newTestTidy(t *testing.T, ollamaURL string) (*Tidy, string) {
	t.Helper()
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	dataDir := filepath.Join(dir, "hfetch")
	t.Setenv("HFETCH_DATA_DIR", dataDir)

	tidy := &Tidy{
		manifestPath: manifestPath,
		provider: &inventory.Provider{
			Ollama: ollama.New(ollamaURL),
			GGUF:   registry.New(dataDir),
		},
	}
	return tidy, dir
}

func ollamaServer(models string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprintln(w, models)
		case "/api/delete":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestSplitRepoQuant(t *testing.T) {
	cases := []struct {
		in              string
		wantRepo, wantQ string
	}{
		{"Org/Repo Q4_K_M", "Org/Repo", "Q4_K_M"},
		{"Org/Repo", "Org/Repo", ""},
		{"llama3.3:70b", "llama3.3:70b", ""},
		{"  Org/Repo  ", "Org/Repo", ""},
	}
	for _, tc := range cases {
		r, q := splitRepoQuant(tc.in)
		if r != tc.wantRepo || q != tc.wantQ {
			t.Errorf("splitRepoQuant(%q) = (%q, %q), want (%q, %q)", tc.in, r, q, tc.wantRepo, tc.wantQ)
		}
	}
}

func TestNearestMatches(t *testing.T) {
	m := &Manifest{Version: 1,
		Ollama: []OllamaModelSpec{{Name: "llama3.3:70b"}, {Name: "qwen3:32b"}},
		GGUF:   []GGUFModelSpec{{Repo: "Org/Llama-Special", Quant: "Q4_K_M"}},
	}
	hits := nearestMatches(m, "llama")
	if len(hits) != 2 {
		t.Fatalf("hits = %v", hits)
	}
	if !contains(hits, "llama3.3:70b") || !contains(hits, "Org/Llama-Special Q4_K_M") {
		t.Errorf("hits = %v", hits)
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	srv := ollamaServer(`{"models":[]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)

	if _, err := tidy.LoadManifest(); err != ErrManifestNotFound {
		t.Errorf("got %v, want ErrManifestNotFound", err)
	}
}

func TestInitWritesFromInventory(t *testing.T) {
	srv := ollamaServer(`{"models":[{"name":"qwen3:32b","size":19000000000}]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)

	m, err := tidy.Init(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ollama) != 1 || m.Ollama[0].Name != "qwen3:32b" {
		t.Errorf("Init manifest = %+v", m)
	}
	if m.Version != manifest.SchemaVersion {
		t.Errorf("Version = %d", m.Version)
	}
	loaded, err := tidy.LoadManifest()
	if err != nil {
		t.Fatalf("load after init: %v", err)
	}
	if len(loaded.Ollama) != 1 {
		t.Errorf("manifest not persisted: %+v", loaded)
	}
}

func TestPromoteOllamaModel(t *testing.T) {
	srv := ollamaServer(`{"models":[{"name":"qwen3:32b","size":19000000000}]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)

	if err := tidy.Promote(context.Background(), "qwen3:32b", BackendOllama); err != nil {
		t.Fatal(err)
	}
	m, err := tidy.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ollama) != 1 || m.Ollama[0].Name != "qwen3:32b" {
		t.Errorf("Ollama specs after promote: %+v", m.Ollama)
	}
}

func TestPromoteRejectsUnknown(t *testing.T) {
	srv := ollamaServer(`{"models":[]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)

	err := tidy.Promote(context.Background(), "ghost:latest", BackendOllama)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("got %v", err)
	}
}

func TestPromoteRejectsDuplicate(t *testing.T) {
	srv := ollamaServer(`{"models":[{"name":"qwen3:32b","size":1}]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)

	ctx := context.Background()
	if err := tidy.Promote(ctx, "qwen3:32b", BackendOllama); err != nil {
		t.Fatal(err)
	}
	err := tidy.Promote(ctx, "qwen3:32b", BackendOllama)
	if err == nil || !strings.Contains(err.Error(), "already in manifest") {
		t.Errorf("got %v", err)
	}
}

func TestDemoteOllama(t *testing.T) {
	srv := ollamaServer(`{"models":[{"name":"qwen3:32b","size":1}]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)
	ctx := context.Background()

	if err := tidy.Promote(ctx, "qwen3:32b", BackendOllama); err != nil {
		t.Fatal(err)
	}
	if err := tidy.Demote(ctx, "qwen3:32b"); err != nil {
		t.Fatal(err)
	}
	m, _ := tidy.LoadManifest()
	if len(m.Ollama) != 0 {
		t.Errorf("expected manifest empty, got %+v", m.Ollama)
	}
}

func TestDemoteMissingSuggests(t *testing.T) {
	srv := ollamaServer(`{"models":[{"name":"qwen3:32b","size":1}]}`)
	defer srv.Close()
	tidy, _ := newTestTidy(t, srv.URL)
	ctx := context.Background()
	_ = tidy.Promote(ctx, "qwen3:32b", BackendOllama)

	err := tidy.Demote(ctx, "qwen")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("expected suggestions, got %v", err)
	}
}

func TestPruneDeletesUntracked(t *testing.T) {
	deleted := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprintln(w, `{"models":[
				{"name":"blessed:latest","size":1000},
				{"name":"trash:latest","size":2000}
			]}`)
		case "/api/delete":
			body := struct{ Name string }{}
			_ = decodeJSON(r, &body)
			deleted[body.Name] = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	tidy, _ := newTestTidy(t, srv.URL)
	ctx := context.Background()
	if err := tidy.Promote(ctx, "blessed:latest", BackendOllama); err != nil {
		t.Fatal(err)
	}

	removed, bytes, err := tidy.Prune(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].OllamaName != "trash:latest" {
		t.Errorf("removed = %+v", removed)
	}
	if bytes != 2000 {
		t.Errorf("bytes = %d", bytes)
	}
	if !deleted["trash:latest"] || deleted["blessed:latest"] {
		t.Errorf("delete calls: %v", deleted)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
