package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lazypower/spark-tools/internal/openaiapi"
)

func TestResolveModel(t *testing.T) {
	ctx := context.Background()

	// An explicit --model wins without touching the network.
	if got := resolveModel(ctx, openaiapi.NewClient("http://127.0.0.1:0"), "my-model"); got != "my-model" {
		t.Errorf("explicit --model must win, got %q", got)
	}

	// Otherwise the server's first advertised model (/v1/models).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"object":"list","data":[{"id":"served-model","object":"model"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if got := resolveModel(ctx, openaiapi.NewClient(srv.URL), ""); got != "served-model" {
		t.Errorf("must adopt the server's advertised model, got %q", got)
	}

	// A server that doesn't implement /v1/models → empty (NEVER the endpoint
	// URL), so the omitempty request field lets the server pick its default.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dead.Close()
	if got := resolveModel(ctx, openaiapi.NewClient(dead.URL), ""); got != "" {
		t.Errorf("an unavailable model list must yield empty, not %q", got)
	}

	// A multi-model gateway → refuse to guess (the first entry may be an
	// embeddings/rerank model that would 400 a chat call): empty, not Data[0].
	multi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"object":"list","data":[{"id":"text-embedding"},{"id":"chat-model"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer multi.Close()
	if got := resolveModel(ctx, openaiapi.NewClient(multi.URL), ""); got != "" {
		t.Errorf("a multi-model server must not be guessed, got %q", got)
	}
}
