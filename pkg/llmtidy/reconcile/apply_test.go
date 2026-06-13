package reconcile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/manifest"
	"github.com/lazypower/spark-tools/pkg/llmtidy/ollama"
)

func TestPruneSuccessAggregatesBytes(t *testing.T) {
	deleted := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := struct{ Name string }{}
		_ = parseJSON(r, &body)
		deleted[body.Name] = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &inventory.Provider{Ollama: ollama.New(srv.URL)}
	plan := []inventory.InstalledModel{
		{Backend: inventory.BackendOllama, OllamaName: "a", Size: 100, Name: "a"},
		{Backend: inventory.BackendOllama, OllamaName: "b", Size: 250, Name: "b"},
	}

	removed, bytes, err := Prune(context.Background(), p, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || bytes != 350 {
		t.Errorf("removed=%d bytes=%d", len(removed), bytes)
	}
	if !deleted["a"] || !deleted["b"] {
		t.Errorf("delete calls: %v", deleted)
	}
}

func TestPrunePartialFailureContinues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := struct{ Name string }{}
		_ = parseJSON(r, &body)
		if body.Name == "bad" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &inventory.Provider{Ollama: ollama.New(srv.URL)}
	plan := []inventory.InstalledModel{
		{Backend: inventory.BackendOllama, OllamaName: "a", Size: 100, Name: "a"},
		{Backend: inventory.BackendOllama, OllamaName: "bad", Size: 50, Name: "bad"},
		{Backend: inventory.BackendOllama, OllamaName: "c", Size: 200, Name: "c"},
	}

	var events []PruneEvent
	removed, bytes, err := Prune(context.Background(), p, plan, func(e PruneEvent) {
		events = append(events, e)
	})
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should name failing model: %v", err)
	}
	if len(removed) != 2 || bytes != 300 {
		t.Errorf("partial removal lost good cases: removed=%d bytes=%d", len(removed), bytes)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

type fakeSyncer struct {
	ollamaCalls []string
	ggufCalls   []string
	failOnRepo  string
}

func (f *fakeSyncer) PullOllama(_ context.Context, name string, onStatus func(string)) error {
	f.ollamaCalls = append(f.ollamaCalls, name)
	if onStatus != nil {
		onStatus("downloading")
	}
	return nil
}

func (f *fakeSyncer) PullGGUF(_ context.Context, repo, quant string, onStatus func(string)) error {
	f.ggufCalls = append(f.ggufCalls, repo+":"+quant)
	if onStatus != nil {
		onStatus("downloading")
	}
	if repo == f.failOnRepo {
		return errors.New("simulated failure")
	}
	return nil
}

func TestSyncDispatchesPerBackend(t *testing.T) {
	syncer := &fakeSyncer{}
	plan := []ModelSpec{
		{Backend: inventory.BackendOllama, Ollama: &manifest.OllamaModelSpec{Name: "qwen3"}},
		{Backend: inventory.BackendGGUF, GGUF: &manifest.GGUFModelSpec{Repo: "org/repo", Quant: "Q4_K_M"}},
	}

	var events []SyncEvent
	if err := Sync(context.Background(), syncer, plan, func(e SyncEvent) {
		events = append(events, e)
	}); err != nil {
		t.Fatal(err)
	}

	if len(syncer.ollamaCalls) != 1 || syncer.ollamaCalls[0] != "qwen3:latest" {
		t.Errorf("ollama calls: %v", syncer.ollamaCalls)
	}
	if len(syncer.ggufCalls) != 1 || syncer.ggufCalls[0] != "org/repo:Q4_K_M" {
		t.Errorf("gguf calls: %v", syncer.ggufCalls)
	}
	if len(events) == 0 {
		t.Error("expected status events")
	}
}

func TestSyncPropagatesPerSpecError(t *testing.T) {
	syncer := &fakeSyncer{failOnRepo: "bad/repo"}
	plan := []ModelSpec{
		{Backend: inventory.BackendOllama, Ollama: &manifest.OllamaModelSpec{Name: "ok"}},
		{Backend: inventory.BackendGGUF, GGUF: &manifest.GGUFModelSpec{Repo: "bad/repo", Quant: "Q4_K_M"}},
		{Backend: inventory.BackendGGUF, GGUF: &manifest.GGUFModelSpec{Repo: "good/repo", Quant: "Q4_K_M"}},
	}

	err := Sync(context.Background(), syncer, plan, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad/repo") {
		t.Errorf("error should name failing spec: %v", err)
	}
	// Successive specs still ran.
	if len(syncer.ggufCalls) != 2 {
		t.Errorf("gguf calls: %v", syncer.ggufCalls)
	}
}

func parseJSON(r *http.Request, v interface{}) error {
	return jsonDecode(r.Body, v)
}
