package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmtidy/ollama"
)

func TestOllamaListConvertsModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"models":[
			{"name":"qwen3:32b","modified_at":"2026-05-01T10:00:00Z","size":19000000000},
			{"name":"llama3.3:70b","modified_at":"2026-04-15T11:00:00Z","size":42000000000}
		]}`)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	models, err := OllamaList(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].Backend != BackendOllama {
		t.Errorf("backend = %v, want Ollama", models[0].Backend)
	}
	if models[0].OllamaName != "qwen3:32b" {
		t.Errorf("OllamaName = %q", models[0].OllamaName)
	}
	if models[0].Size != 19000000000 {
		t.Errorf("Size = %d", models[0].Size)
	}
}

func TestOllamaDeleteRouting(t *testing.T) {
	var gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct{ Name string }
		_ = json.Unmarshal(body, &p)
		gotName = p.Name
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	err := OllamaDelete(context.Background(), c, InstalledModel{
		Backend:    BackendOllama,
		OllamaName: "qwen3:32b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "qwen3:32b" {
		t.Errorf("delete sent name %q", gotName)
	}
}
