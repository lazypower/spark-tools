package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/auth"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	client := NewClient(WithBaseURL(srv.URL), WithToken("hf_test"))
	return srv, client
}

func TestWhoAmI(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/whoami" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer hf_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(auth.UserInfo{
			Username: "testuser",
			FullName: "Test User",
		})
	})
	defer srv.Close()

	info, err := client.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if info.Username != "testuser" {
		t.Errorf("expected testuser, got %q", info.Username)
	}
}

func TestWhoAmI_NoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL)) // no token
	_, err := client.WhoAmI(context.Background())
	if !errors.Is(err, auth.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", err)
	}
}

func TestWhoAmI_InvalidToken(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	_, err := client.WhoAmI(context.Background())
	if !errors.Is(err, auth.ErrAuthInvalid) {
		t.Errorf("expected ErrAuthInvalid, got %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("search") != "llama" {
			t.Errorf("expected search=llama, got %q", q.Get("search"))
		}
		if q.Get("limit") != "5" {
			t.Errorf("expected limit=5, got %q", q.Get("limit"))
		}
		json.NewEncoder(w).Encode([]Model{
			{ID: "meta-llama/Llama-2-7B", Downloads: 1000},
			{ID: "TheBloke/Llama-2-7B-GGUF", Downloads: 500},
		})
	})
	defer srv.Close()

	models, err := client.Search(context.Background(), "llama", SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestGetModel(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/TheBloke/Llama-2-7B-GGUF" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(Model{
			ID:     "TheBloke/Llama-2-7B-GGUF",
			Author: "TheBloke",
		})
	})
	defer srv.Close()

	model, err := client.GetModel(context.Background(), "TheBloke/Llama-2-7B-GGUF")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	if model.ID != "TheBloke/Llama-2-7B-GGUF" {
		t.Errorf("expected TheBloke/Llama-2-7B-GGUF, got %q", model.ID)
	}
}

func TestListFiles(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/TheBloke/Llama-2-7B-GGUF/tree/main" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]ModelFile{
			{Filename: "llama-2-7b.Q4_K_M.gguf", Size: 4_000_000_000},
			{Filename: "llama-2-7b.Q8_0.gguf", Size: 7_000_000_000},
			{Filename: "README.md", Size: 1234},
		})
	})
	defer srv.Close()

	files, err := client.ListFiles(context.Background(), "TheBloke/Llama-2-7B-GGUF")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestGatedModel(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	defer srv.Close()

	_, err := client.GetModel(context.Background(), "meta-llama/Llama-2-7B")
	if !errors.Is(err, auth.ErrGatedModel) {
		t.Errorf("expected ErrGatedModel, got %v", err)
	}
}

func TestRetryOn500(t *testing.T) {
	attempts := 0
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(Model{ID: "test/model"})
	})
	defer srv.Close()

	model, err := client.GetModel(context.Background(), "test/model")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if model.ID != "test/model" {
		t.Errorf("expected test/model, got %q", model.ID)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
