package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", DefaultHost},
		{"localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
		{"https://ollama.internal", "https://ollama.internal"},
	}
	for _, tc := range cases {
		if got := normalizeHost(tc.in); got != tc.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv(EnvHost, "ollama.lan:11434")
	c := NewFromEnv()
	if c.Host() != "http://ollama.lan:11434" {
		t.Errorf("Host = %q", c.Host())
	}

	t.Setenv(EnvHost, "")
	c = NewFromEnv()
	if c.Host() != DefaultHost {
		t.Errorf("Host = %q, want %q", c.Host(), DefaultHost)
	}
}

func TestAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"models":[]}`)
	}))
	defer srv.Close()

	if !New(srv.URL).Available(context.Background()) {
		t.Fatal("Available should return true for OK response")
	}

	if New("http://127.0.0.1:1").Available(context.Background()) {
		t.Fatal("Available should return false for unreachable host")
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprintln(w, `{"models":[{"name":"qwen3:32b","modified_at":"2026-05-01T10:00:00Z","size":19000000000,"digest":"abc"}]}`)
	}))
	defer srv.Close()

	models, err := New(srv.URL).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "qwen3:32b" || models[0].Size != 19000000000 {
		t.Errorf("unexpected models: %+v", models)
	}
	if models[0].ModifiedAt.Year() != 2026 {
		t.Errorf("ModifiedAt not parsed: %v", models[0].ModifiedAt)
	}
}

func TestListModelsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := New(srv.URL).ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDelete(t *testing.T) {
	var gotBody deleteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/delete" || r.Method != http.MethodDelete {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := New(srv.URL).Delete(context.Background(), "qwen3:32b"); err != nil {
		t.Fatal(err)
	}
	if gotBody.Name != "qwen3:32b" {
		t.Errorf("body name = %q, want %q", gotBody.Name, "qwen3:32b")
	}
}

func TestDeleteServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "model not found")
	}))
	defer srv.Close()

	err := New(srv.URL).Delete(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestPullStreamsStatusEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"status":"downloading"}`)
		fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer srv.Close()

	var statuses []string
	err := New(srv.URL).Pull(context.Background(), "qwen3:32b", func(p PullProgress) {
		statuses = append(statuses, p.Status)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 || statuses[0] != "pulling manifest" || statuses[2] != "success" {
		t.Errorf("unexpected statuses: %v", statuses)
	}
}

func TestPullPropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"error":"manifest not found"}`)
	}))
	defer srv.Close()

	err := New(srv.URL).Pull(context.Background(), "missing", nil)
	if err == nil || !strings.Contains(err.Error(), "manifest not found") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestAvailableTimeoutFastFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	start := time.Now()
	ok := New(srv.URL).Available(context.Background())
	elapsed := time.Since(start)
	if ok {
		t.Fatal("expected Available to fail")
	}
	if elapsed > 4*time.Second {
		t.Errorf("Available should fast-fail; took %v", elapsed)
	}
}
